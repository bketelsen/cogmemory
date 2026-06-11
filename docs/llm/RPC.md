# cogmemory JSON-RPC Reference

Canonical per-method contract for the cogmemory daemon. Supersedes
`docs/SKILL-REWRITES.md` as the integration contract. Every param name and
response field below is extracted from the Go structs' json tags in
`rpc/*.go` and `store/*.go`.

## Transport

- JSON-RPC 2.0 over a Unix domain socket.
- **Newline-delimited framing**: one request object per line, one response
  object per line. No batching.
- Connections are persistent; requests on a single connection are processed
  **sequentially** (the server reads line → dispatches → writes response →
  reads next line). Separate connections run concurrently.
- **~64 KB request line limit** (`bufio.Scanner` default token size). A
  request line exceeding it terminates the read loop: the connection is
  closed with **no response at all**. Keep large writes under this bound or
  split them into `append` calls.
- A missing `"jsonrpc"` field is tolerated (treated as `"2.0"`). `id` is
  echoed back verbatim.

Request shape:

```json
{"jsonrpc":"2.0","id":1,"method":"read","params":{"role":"siona","path":"hot-memory.md"}}
```

## Error codes

| Code | Constant | Meaning |
|---|---|---|
| -32700 | `CodeParseError` | request line is not valid JSON |
| -32600 | `CodeInvalidRequest` | malformed JSON-RPC request |
| -32601 | `CodeMethodNotFound` | unknown method |
| -32602 | `CodeInvalidParams` | missing/invalid params (incl. id-as-path write rejection) |
| -32000 | `CodeRBACDenied` | role lacks read/write on the target path |
| -32001 | `CodeStoreError` | store-layer failure (missing section, patch mismatch, git failure, format validation, ...) |

Error object: `{"code": int, "message": string}`.

## Roles & RBAC

Every method's params accept a `"role"` string; it is omitted from the
per-method tables below. RBAC is a per-path, first-match-wins pattern check
(doublestar globs) with **deny by default** — unknown roles and unmatched
paths are denied. Denial returns `-32000`.

- Single-file methods check the role against the request path (`read` op for
  read-like methods, `write` op for write-like methods; `move` checks write
  on the **destination** only).
- Aggregate methods (`open_actions`, `recent_observations`, `cluster_check`,
  `scenario_check`, `session_brief`, `housekeeping_scan`,
  `glacier_index_compute`, `wiki_index_compute`, `domain_summary`,
  `entity_audit`, `link_index_compute`, `link_audit`) **silently filter** out
  files/entries the role cannot read instead of erroring. These methods also
  require a non-empty `role` and return `-32602 "... role required"` without
  one.
- `stats`, `l0index`, `list`, `health` perform **no** RBAC check (metadata
  only). `git` checks RBAC only for `op:"commit"` (write on `**`).
- `read` of the special paths `L0_INDEX` and `LIST` bypasses RBAC.

---

## File operations

### read

Read a file, a section of it, or a line range. Missing files are not an
error: they return `content:""`, `found:false`.

**Params**

| name | type | required | notes |
|---|---|---|---|
| path | string | yes | relative path under memory root; special values `L0_INDEX` (returns L0 index as content) and `LIST` (newline-joined path list) bypass RBAC |
| section | string | no | markdown heading, `"## Open"` or `"Open"` (normalized to `## `, case-insensitive match); section runs until the next `##`-prefixed line |
| start | int | no | 1-based inclusive start line (ignored when `section` set) |
| end | int | no | 1-based inclusive end line; 0 = EOF |

**Result**

```json
{
  "content": "...",   // string; "" when file missing or empty
  "found": true       // bool; false iff content == ""
}
```

**Errors**: `-32001` when `section` is given but the heading is not found
(`store: section not found in ...`). Path traversal (`..`) → `-32001`.

### write

Atomically replace a file's full content (temp file → fsync → rename).
Creates parent directories.

**Params**

| name | type | required | notes |
|---|---|---|---|
| path | string | yes | |
| content | string | yes | full new file content |

**Result**

```json
{ "bytes": 42 }   // int, byte length of the content written
```

**Errors**: writes whose first path segment is a **domain id** whose
configured path lives elsewhere (e.g. `chapterhouse/INDEX.md` when domain
`chapterhouse` lives at `projects/chapterhouse`) are rejected with `-32602`
and a corrective message of the form:
`write to "chapterhouse/INDEX.md" uses domain id "chapterhouse" as its path; domain "chapterhouse" lives at "projects/chapterhouse"`.
Other manifest-hygiene violations (undeclared file under a valid domain
path) are log-only warnings, not errors.

### append

Append text to a file (created if absent), optionally inside a named
markdown section.

**Params**

| name | type | required | notes |
|---|---|---|---|
| path | string | yes | |
| text | string | yes | trailing newline added if missing |
| section | string | no | heading text (`"Open"` or `"## Open"`, any `#` level, case-insensitive); text is inserted at the end of that section, before the next heading at the same-or-shallower level |

**Result**

```json
{ "ok": true }
```

**Errors**:
- Same id-as-path rejection as `write` (`-32602`).
- Paths ending in `observations.md`: every non-blank line of `text` must
  match `- YYYY-MM-DD [tags]: text` (regex
  `^-\s+\d{4}-\d{2}-\d{2}\s+\[.+\]:\s*.+$`). Violations → `-32001` with the
  invalid lines listed.
- `section` given but file does not exist → `-32001` (`create it first`).
- `section` given but heading not found → `-32001` (`heading not found`) —
  the daemon never silently lands content at EOF.

### patch

Surgical single-occurrence string replacement.

**Params**

| name | type | required | notes |
|---|---|---|---|
| path | string | yes | |
| old_text | string | yes | must appear **exactly once** |
| new_text | string | yes | |

**Result**

```json
{ "ok": true }
```

**Errors**: `-32001` when `old_text` appears 0 times or ≥2 times (message
includes the count); `-32001` when the file does not exist.

### outline

List level-2 and level-3 markdown headings in a file.

**Params**

| name | type | required | notes |
|---|---|---|---|
| path | string | yes | RBAC `read` check |

**Result**

```json
{
  "entries": [
    { "line": 3, "text": "Open", "level": 2 }   // level is 2 or 3
  ]
}
```

`entries` is `null` (not `[]`) when the file has no `##`/`###` headings.
Missing file → `-32001`.

### move

Rename a file within the memory root. RBAC: `write` on `to` (the `from`
path is **not** checked).

**Params**

| name | type | required | notes |
|---|---|---|---|
| from | string | yes | |
| to | string | yes | parent dirs created; existing destination → `-32001` (`move destination exists`) |

**Result**

```json
{ "ok": true }
```

### search

Case-insensitive substring search across every file under the root.
RBAC: requires read on `**` (or, failing that, read on `hot-memory.md`);
results are **not** per-path filtered after that gate.

**Params**

| name | type | required | notes |
|---|---|---|---|
| query | string | yes | plain substring, not regex |

**Result**

```json
{
  "results": [
    { "path": "personal/observations.md", "line": 12, "text": "the matching line" }
  ],
  "count": 1
}
```

`results` is `null` when there are no matches (`count: 0`).

### list

All relative file paths under the memory root (excludes `.git/` and `*.tmp`).
No RBAC check.

**Params**: none beyond `role` (which is ignored).

**Result**

```json
{ "paths": ["hot-memory.md", "personal/observations.md"] }
```

### stats

File/line/size statistics. No RBAC check.

**Params**

| name | type | required | notes |
|---|---|---|---|
| prefix | string | no | restrict to files whose path equals `prefix` or starts with `prefix/` (leading/trailing slashes trimmed); totals reflect the matched subset |

**Result** (bare envelope, no wrapper key)

```json
{
  "files": 12,
  "lines": 3401,
  "size": 90211,
  "per_file": [
    { "path": "hot-memory.md", "lines": 40, "size": 1801, "modified": "2026-06-09T18:00:00Z" }
  ]
}
```

`per_file` is always non-nil and sorted by path; `modified` is RFC3339 UTC.

### l0index

Index of L0 summary headers. No RBAC check. **Silently omits** any file
whose **first line** does not contain an `<!-- L0: summary -->` comment —
absence from the index does not mean the file doesn't exist.

**Params**

| name | type | required | notes |
|---|---|---|---|
| domain | string | no | path-prefix filter (matches lines starting `<domain>/`); note this filters by **directory prefix**, not canonical domain id |

**Result**

```json
{ "index": "hot-memory.md: current focus and context\npersonal/observations.md: dated observations" }
```

`index` is a single string of `path: summary` lines joined by `\n` (empty
string when nothing has an L0 header).

---

## Consolidated envelopes

### open_actions

All unchecked `- [ ] ` items across the action-items files declared in
`domains.yml` (controller-resolved), with parsed `| key:value` metadata.
Lines inside HTML comments and code fences are skipped. Missing files are
skipped silently. Items are RBAC-filtered per path.

**Params**

| name | type | required | notes |
|---|---|---|---|
| domain | string | no | restrict to one canonical domain id; unknown id or domain not declaring `action-items` → `-32602` |

**Result**

```json
{
  "items": [
    {
      "domain": "personal",
      "path": "personal/action-items.md",
      "line": 7,
      "text": "renew passport",
      "raw": "- [ ] renew passport | due:2026-07-01 | pri:high | added:2026-06-01",
      "due": "2026-07-01",        // omitted when absent
      "priority": "high",         // from "pri:" or "priority:"; omitted when absent
      "added": "2026-06-01"       // omitted when absent
    }
  ]
}
```

### session_brief

One-call session-start orientation: root hot-memory + patterns + visible
domains + per-domain open-action counts.

**Params**: `role` only.

**Result**

```json
{
  "hot_memory": "<content of hot-memory.md>",            // "" when absent; always returned
  "patterns": "<content of cog-meta/patterns.md>",       // "" when absent; always returned
  "domains": [
    {
      "id": "personal",
      "path": "personal",                  // always present
      "label": "Personal life",            // omitted when empty
      "triggers": ["family", "health"]     // omitted when empty
    }
  ],
  "action_counts": {
    "personal": 4,                          // open-count per readable domain id
    "_pri_high_anywhere": false             // bool; true if any readable domain has a pri:high open item
  },
  "controller_last_error": null             // null, or string with the last domains.yml hot-reload error
}
```

`domains` is a flattened list (subdomains included as their own entries),
RBAC-filtered per domain path. `action_counts` only contains domains the
role can read, plus the `_pri_high_anywhere` key.

### housekeeping_scan

Cap/threshold scan over all domains plus the root-level conventional files
(`hot-memory.md`, `cog-meta/patterns.md`, `cog-meta/improvements.md`).
File locations and caps are hard-coded server-side.

**Params**: `role` only.

Default caps: observations entries 50, completed actions 10, improvements
implemented 10, hot-memory lines 50, patterns 70 lines / 5500 bytes,
dormancy window 28 days, stale action item 14 days, changed-recently
fallback 7 days.

**Result** (bare envelope)

```json
{
  "since": "2026-06-03T10:00:00Z",      // RFC3339 mtime of cog-meta/.housekeeping-marker; "" when no marker (then the changed_recently window is now-7d)
  "changed_recently": ["personal/observations.md"],   // files modified after `since`, RBAC-filtered, sorted; marker itself excluded
  "thresholds": {
    "observations_over_cap": [
      { "path": "personal/observations.md", "entries": 61, "cap": 50,
        "by_primary_tag": { "health": 20, "(untagged)": 2 } }   // count by FIRST tag in each entry's bracket list
    ],
    "completed_actions_over_cap": [
      { "path": "personal/action-items.md", "completed": 14, "cap": 10 }
    ],
    "improvements_implemented_over_cap": [
      { "path": "cog-meta/improvements.md", "implemented": 12, "cap": 10 }
    ],
    "hot_memory_over_cap": [
      { "path": "hot-memory.md", "lines": 58, "cap": 50 }
    ],
    "patterns_over_cap": [
      { "path": "cog-meta/patterns.md", "lines": 80, "size": 6000,
        "lines_cap": 70, "size_cap": 5500 }       // dual-axis; either or both may exceed
    ]
  },
  "dormant_domains": [
    { "id": "hobby", "last_observation": "2026-04-01" }   // "" when the file has no parseable dated entry
  ],
  "stale_action_items": [
    { "path": "personal/action-items.md", "line": 9,
      "text": "renew passport", "added": "2026-05-01", "age_days": 40 }
    // only open items with an added:YYYY-MM-DD marker; ageless items are skipped
  ]
}
```

All arrays are non-nil (`[]` when empty).

### recent_observations

Parsed observation entries (`- YYYY-MM-DD [tags]: text`) across all
controller-declared observations files, with pre-computed aggregates.

**Params**

| name | type | required | notes |
|---|---|---|---|
| since | string | no | `YYYY-MM-DD`, RFC3339, Go duration (`168h`), or `"Nd"` (e.g. `7d`, `90d`) — same forms as cluster_check/domain_summary. Inclusive lower bound (`date >= since`). Default: today (UTC) minus 7 days |
| by_tag | string | no | only entries whose tag list contains this tag (case-sensitive); aggregates reflect the filtered set |
| by_domain | string | no | restrict to one canonical domain id; unknown id or domain not declaring `observations` → `-32602` |

**Result** (bare envelope)

```json
{
  "since": "2026-06-03",
  "entries": [
    { "domain": "personal", "path": "personal/observations.md", "line": 31,
      "date": "2026-06-09", "tags": ["health", "sleep"], "text": "slept badly" }
  ],
  "by_domain": { "personal": 1 },   // counts over the returned entries
  "by_tag": { "health": 1, "sleep": 1 }
}
```

Entries are sorted newest-first (ties by path, then line). All collections
are non-nil. Lines inside HTML comments / code fences are skipped.

### cluster_check

Tag/keyword clustering over recent observations to surface recurring topics.
Keyword extraction is naive token frequency (lowercased tokens of 4+ word
chars, stopword-filtered) — not semantic.

**Params**

| name | type | required | notes |
|---|---|---|---|
| domain | string | no | restrict to one canonical domain id |
| min_cluster_size | int | no | default 3 |
| since | string | no | accepts `"Nd"` (e.g. `7d`, `90d`), Go duration (`168h`), `YYYY-MM-DD`, or RFC3339. Default: now − 7d |
| span_days | int | no | thread-candidate minimum span; default 14 |
| sample_limit | int | no | samples per cluster; default 3 |

**Result** (bare envelope)

```json
{
  "by_tag": [
    { "tag": "health", "count": 5, "spans_days": 9,
      "domains": ["personal"],
      "samples": [
        { "date": "2026-06-09", "domain": "personal",
          "path": "personal/observations.md", "line": 31, "text": "slept badly" }
      ] }
  ],
  "by_keyword": [
    { "keyword": "sleep", "count": 4, "spans_days": 8,
      "domains": ["personal"], "samples": [ /* same SampleObs shape */ ] }
  ],
  "thread_candidates": [
    { "topic": "tag:health",            // "tag:<t>" or "keyword:<k>"
      "fragment_count": 5,
      "date_range": "2026-06-01..2026-06-09" }
  ]
}
```

Tags are lowercased/deduped; clusters sorted by count desc. Samples are the
newest observations, capped at `sample_limit`.

### scenario_check

Schedule of active scenario files in `cog-meta/scenarios/*.md`, judged
against their `check-by:` frontmatter date.

**Params**: `role` only.

**Result**

```json
{
  "scenarios": [
    { "path": "cog-meta/scenarios/job-change.md",
      "check_by": "2026-06-15",
      "status": "active",            // "active" | "due_now" | "overdue"
      "days_until_check": 5 }        // negative when overdue, 0 when due_now
  ]
}
```

Only scenarios with frontmatter `status: active` (or no status field) and a
parseable `check-by: YYYY-MM-DD` appear; everything else is skipped
silently. RBAC-filtered per file path.

### domain_summary

One-call summary of a single domain: hot memory, action counts, recent
observations, presence/recency metadata.

**Params**

| name | type | required | notes |
|---|---|---|---|
| domain | string | yes | canonical domain id; unknown → `-32602` |
| since | string | no | `YYYY-MM-DD`, RFC3339, Go duration, or `"Nd"`. Default: 7 days ago |

RBAC: the role must read the domain's declared path (else `-32000` for the
whole call); individual declared files the role can't read are skipped
silently.

**Result** (bare envelope)

```json
{
  "domain": "personal",
  "path": "personal",
  "label": "Personal life",
  "hot_memory": "<content of personal/hot-memory.md>",    // "" when absent/undeclared
  "open_action_count": 4,
  "completed_action_count_since": 2,    // "- [x]" items with added:>= since; undated completed items are NOT counted
  "recent_observations": [
    { "domain": "", "path": "personal/observations.md", "line": 31,
      "date": "2026-06-09", "tags": ["health"], "text": "..." }
    // note: the per-entry "domain" field is "" in this envelope (single-file helper)
  ],
  "files_present": ["hot-memory", "observations", "action-items"],   // declared basenames that exist on disk (and are readable)
  "last_activity": "2026-06-09",   // max(file mtimes, newest observation date), YYYY-MM-DD; "" when nothing found
  "since": "2026-06-03"            // resolved YYYY-MM-DD floor actually used
}
```

### entity_audit

Hygiene audit of `entities.md` files (controller-declared `entities`).
Missing files skipped silently; targets RBAC-filtered up front.

**Params**

| name | type | required | notes |
|---|---|---|---|
| domain | string | no | restrict to one canonical domain id; unknown id or domain not declaring `entities` → `-32602` |

**Result** (bare envelope; all arrays non-nil)

```json
{
  "format_violations": [
    { "path": "personal/entities.md", "domain": "personal",
      "name": "Microsoft",            // parenthetical suffix stripped: "Microsoft (employer)" -> "Microsoft"
      "lines": 5,                     // heading + non-blank non-comment body lines; flagged when > 3
      "issue": "exceeds_3_line_compact",
      "has_detail_file": true }       // block contains a [[wiki:...]] link
  ],
  "glacier_candidates": [
    { "path": "personal/entities.md", "domain": "personal", "name": "OldCo",
      "status": "inactive",           // omitted when absent
      "last": "2025-10-01",           // omitted when absent
      "age_days": 252 }               // -1 when `last` missing/unparseable; omitted when exactly 0; flagged when status==inactive or age_days > 180
  ],
  "missing_metadata": [
    { "path": "personal/entities.md", "domain": "personal", "name": "NewCo",
      "missing": ["status", "last"] }
  ],
  "temporal_violations": [
    { "path": "personal/entities.md", "domain": "personal", "name": "Microsoft",
      "line": 14, "text": "role: PM (until 2026-04)",
      "needs": "strikethrough" }      // "(until YYYY-MM)" whose month has passed and isn't ~~struck~~
  ],
  "total_entries": 12,   // count of ### entity blocks scanned
  "total_lines": 31      // sum of counted lines across blocks; lines/entries feeds the compression ratio (target <= 3.0)
}
```

### glacier_index_compute

Index of `glacier/**/*.md` with parsed YAML frontmatter. Files without
parseable frontmatter still appear with only `path` set. RBAC-filtered.

**Params**: `role` only.

**Result**

```json
{
  "entries": [
    { "path": "glacier/personal/observations-2025.md",
      "domain": "personal",                 // omitted when absent
      "type": "observations-archive",       // omitted when absent
      "tags": ["health"],                   // always present, [] when none
      "date_range": "2025-01..2025-12",     // omitted when absent
      "entries": 48,                        // omitted when 0
      "summary": "..." }                    // omitted when absent
  ],
  "count": 1
}
```

Empty slice (not null) when `glacier/` is absent. Sorted by path.

### wiki_index_compute

Index of `wiki/**/*.md` content pages with parsed YAML frontmatter.
Excludes the generated catalog `wiki/index.md` and registry files under
`wiki/_meta/`. RBAC-filtered.

**Params**: `role` only.

**Result**

```json
{
  "entries": [
    { "path": "wiki/people/jane-smith.md",
      "category": "person",            // from frontmatter `entity_type`; omitted when absent
      "title": "Jane Smith",           // omitted when absent
      "status": "active",              // omitted when absent
      "tags": ["work"],                // always present, [] when none
      "summary": "...",                // omitted when absent
      "updated": "2026-06-01",         // omitted when absent
      "related": ["wiki/orgs/acme"] }  // omitted when absent
  ],
  "count": 1
}
```

### link_index_compute

Reverse `[[wiki-link]]` index over every markdown file: target → sources.
Frontmatter `related:` entries are indexed as links too. Source paths are
extensionless (e.g. `personal/observations`); targets are as-encoded after
stripping `#section` and a trailing `.md`. Self-references are dropped.
Sources the role can't read are removed; targets that lose all sources are
dropped.

**Params**: `role` only.

**Result**

```json
{
  "links": [
    { "target": "personal/entities#Jane",
      "sources": ["personal/observations", "wiki/people/jane-smith"] }
  ]
}
```

### link_audit

Suspected missing links: unlinked whole-word mentions (case-sensitive) of
entity names whose canonical home is a `### Name` header in some
`*/entities.md`. Mentions already inside a `[[...]]` span and mentions in
the entity's own home file are skipped. Both entity homes and source files
are RBAC-filtered.

**Params**: `role` only.

**Result**

```json
{
  "candidates": [
    { "source_path": "personal/observations.md",
      "line": 12,
      "entity_name": "Jane Smith",
      "target_link": "personal/entities#Jane Smith",
      "context": "Lunch with Jane Smith about the move" }
  ]
}
```

---

## Admin

### domains.list

Top-level domains from `domains.yml` (hot-reloaded on mtime change).
RBAC-filtered on each **top-level** domain's path; subdomains ride along
nested inside their parent (unlike `session_brief`, which flattens and
filters per subdomain).

**Params**: `role` only.

**Result**

```json
{
  "domains": [
    {
      "id": "work",
      "path": "work",
      "label": "Work",                  // always present (may be "")
      "type": "area",                   // omitted when empty
      "triggers": ["job"],              // omitted when empty
      "files": ["observations", "action-items"],  // declared basenames, no .md; omitted when empty
      "subdomains": [ /* same Domain shape, recursive */ ]  // omitted when empty
    }
  ]
}
```

**Errors**: `-32001 "controller unavailable"` when the daemon was started
without a domain controller.

### domains.get

One domain by canonical id (matches top-level and subdomains).

**Params**

| name | type | required | notes |
|---|---|---|---|
| id | string | yes | unknown id → `-32001` |

**Result**

```json
{ "domain": { "id": "work", "path": "work", "label": "Work", "...": "same Domain shape as domains.list" } }
```

RBAC: read on the domain's path, else `-32000`.

### health

Liveness probe. No params required (role ignored), no RBAC.

**Result**

```json
{ "ok": true }
```

### git

Run a git operation in the memory root. RBAC: only `op:"commit"` requires
write on `**`; `status`/`diff`/`log` have no RBAC check.

**Params**

| name | type | required | notes |
|---|---|---|---|
| op | string | yes | `"status"` (`git status --short`), `"diff"`, `"log"` (`--oneline`), `"commit"` (auto-stages with `git add -A`) |
| ref | string | no | for `diff`/`log` |
| message | string | for commit | commit without message → `-32001` |
| paths | []string | no | for `diff` (passed after `--`) |
| limit | int | no | for `log`; default 20 |

**Result**

```json
{ "output": "<trimmed stdout+stderr of the git command>" }
```

**Errors**: unknown `op`, git not installed, or non-zero git exit →
`-32001` (message includes git's output).
