# RPC Consolidation: Named Patterns from Cog Skills

Audit of `marciopuga/cog`'s `.claude/commands/*.md` skill bodies to find load-bearing read/write patterns that recur in the same shape every run. Each pattern is a candidate for a single typed RPC that replaces N round-trips while preserving the LLM in the loop.

## Premise

`open_actions` (PR #3) showed the shape. Every cog skill currently:

1. Tells the agent "run these shell/RPC commands"
2. The agent executes N round-trips (one per file or per domain)
3. The agent assembles the result locally
4. The agent reasons over it

Step 4 is where the LLM earns its keep. Steps 1–3 are mechanical convention dressed up as instructions. Anything that runs in the same order every time, on the same file shapes, against conventions the daemon already knows about, is a candidate for collapse.

**Goal: fewer round-trips and a single point of enforcement (RBAC, schema, ordering). Not removing the LLM.**

## Catalog of Named Patterns

For each pattern: where it lives in cog-prime today, what it does, the call count it replaces, the proposed RPC signature, and a sketch of the result envelope.

---

### 1. `session_brief`

**Today** (`CLAUDE.md` Memory Rules §1, every skill's "Read on activation" block):
> Always read `memory/hot-memory.md` and `memory/cog-meta/patterns.md` at conversation start.

Plus every skill reads `domains.yml` to know what exists, and several read `cog-meta/reflect-cursor.md` or `briefing-bridge.md` as part of setup.

**Round-trips today**: 2–5 reads at session start, every consumer reimplements the convention.

**Proposed RPC**: `session_brief(role)`

**Result envelope**:
```json
{
  "hot_memory": "<content of memory/hot-memory.md>",
  "patterns": "<content of memory/cog-meta/patterns.md>",
  "domains": [{"id": "...", "label": "...", "triggers": ["..."]}, ...],
  "action_counts": {"<domain>": N, "_pri_high_anywhere": bool},
  "controller_last_error": null | "<message>"
}
```

**RBAC**: filter `domains` and `action_counts` to what the role can read. Always return `hot_memory` + `patterns` (they're owner-canonical files; reading them is the contract).

**Out of scope**: per-domain hot-memory (daemon shouldn't judge relevance), recent observations (reflect's domain), briefing-bridge body (foresight's surface).

**Win**: every session starts with one call instead of 2–5. Consumers stop hardcoding paths. Drift impossible.

---

### 2. `housekeeping_scan`

**Today** (`housekeeping.md` §0 + §1 + §2):
> ```bash
> find memory/ -type f -name "*.md" -mtime -1
> grep -c "^- " memory/cog-meta/self-observations.md memory/personal/observations.md ...
> grep -c "^\- \[x\]" memory/personal/action-items.md memory/*/action-items.md ...
> ```
> Then: "If any `observations.md` has >50 entries..." and "If any `action-items.md` has >10 completed items..." and "Keep ALL hot-memory.md files under 50 lines"

**Round-trips today**: 3 shell scans + 1 read per candidate file to verify thresholds + N reads to perform the archival. A full housekeeping pass today is dozens of round-trips just for the *scan*.

**Proposed RPC**: `housekeeping_scan(role)`

**Result envelope**:
```json
{
  "since": "<ISO timestamp of last housekeeping marker>",
  "changed_recently": ["<relPath>", ...],
  "thresholds": {
    "observations_over_cap": [
      {"path": "personal/observations.md", "entries": 87, "cap": 50,
       "by_primary_tag": {"health": 22, "milestone": 18, ...}}
    ],
    "completed_actions_over_cap": [
      {"path": "personal/action-items.md", "completed": 14, "cap": 10}
    ],
    "improvements_implemented_over_cap": [...],
    "hot_memory_over_cap": [
      {"path": "personal/hot-memory.md", "lines": 63, "cap": 50}
    ]
  },
  "dormant_domains": [
    {"id": "work/acme", "last_observation": "2026-04-01"}
  ],
  "stale_action_items": [
    {"path": "...", "line": 12, "text": "...", "added": "...", "age_days": 22}
  ]
}
```

**RBAC**: per-path, like `open_actions`. A role only sees thresholds on files it can read.

**Win**: housekeeping's first phase ("orientation") becomes one call. The LLM still decides what to *do* about each threshold breach (archive by primary tag, split by year, etc.), which is the part that wants judgment. The "is this over the line?" check is mechanical and the daemon already knows the caps.

**Note on the by-primary-tag breakdown**: housekeeping groups oldest entries by primary tag for glacier archival. Pre-computing this in the scan lets the agent jump straight to archival decisions instead of re-parsing every observation line client-side.

---

### 3. `link_audit` / `link_index`

**Today** (`housekeeping.md` §5 + §6):
> "For each non-glacier memory file: scan for names matching `### <Name>` headers in entities.md — add `[[links]]` if missing"
> Plus the link-index regeneration: read every file, find every `[[link]]`, invert.

**Round-trips today**: O(files × entities) for the audit + O(files) read for the index rebuild. Easily 100+ on a mature memory tree.

**Proposed RPC**: two related calls.

**`link_index_compute(role)`** → returns the reverse index without persisting:
```json
{
  "links": [
    {"target": "personal/entities", "sources": ["personal/observations", "personal/hot-memory"]},
    ...
  ]
}
```

**`link_audit(role)`** → returns missing-link candidates:
```json
{
  "candidates": [
    {"source_path": "personal/observations.md", "line": 14,
     "entity_name": "Jane", "target_link": "personal/entities#Jane",
     "context": "<the matched line>"}
  ]
}
```

**Win**: daemon does the file walks once. The agent decides which candidates are substantive enough to actually patch (the link audit explicitly says "only add links where the reference is substantive" — that's a judgment call kept with the LLM). Index rebuild becomes a single call the agent can then write to `link-index.md` (or, better: a `link_index_write` companion that does both compute and persist atomically — but that's a v2 polish).

---

### 4. `glacier_index_compute`

**Today** (`housekeeping.md` §4):
> "Scan all `memory/glacier/**/*.md` files. Extract YAML frontmatter. Write results to `memory/glacier/index.md`"

**Round-trips today**: 1 list + N reads + 1 write.

**Proposed RPC**: `glacier_index_compute(role)`

**Result envelope**:
```json
{
  "entries": [
    {"path": "glacier/personal/observations-health.md",
     "domain": "personal", "type": "observations", "tags": ["health"],
     "date_range": "2025-01 to 2025-06", "entries": 42,
     "summary": "..."}
  ]
}
```

**RBAC**: per glacier-path read permission.

**Win**: one call instead of N. Frontmatter parsing happens daemon-side (Go YAML is faster and less error-prone than each consumer reimplementing).

**Symmetry note**: same shape as `link_index_compute` — both are "walk a tree, parse a structured header, return tabular result." Tempting to abstract into one `index_compute(kind=glacier|links|domain)` RPC. Resist for v1: the three differ enough in their RBAC and field shape that a typed-per-kind surface is cleaner. Revisit if a fourth index of the same shape appears.

---

### 5. `domain_summary`

**Today** (`reflect.md` §2 + `foresight.md` Memory Files):
> Reflect: "Read each domain's `hot-memory.md`. For every factual claim, read the canonical source file and verify."
> Foresight: "For each domain, read `hot-memory.md` and `action-items.md` (if they exist) ... `memory/personal/entities.md`, `memory/personal/calendar.md`, `memory/personal/health.md` ... Recent observations across all domains (last 7 days)."

**Round-trips today**: 4–8 reads × N domains. On a 6-domain setup, that's 24–48 reads just to lay the table for reflect or foresight.

**Proposed RPC**: `domain_summary(role, domain, since?)`

**Result envelope**:
```json
{
  "domain": "work/acme",
  "label": "Acme work",
  "hot_memory": "<content>",
  "open_action_count": 4,
  "completed_action_count_since": 2,
  "recent_observations": [
    {"date": "2026-05-28", "tags": ["milestone"], "text": "..."},
    ...
  ],
  "files_present": ["hot-memory.md", "action-items.md", "observations.md", "entities.md", ...],
  "last_activity": "2026-05-28"
}
```

`since` defaults to "last 7 days" (reflect + foresight's standard window) but is overridable.

**RBAC**: per-domain. A role without read on the domain path gets `CodeRBACDenied`.

**Win**: foresight's "Memory Files" block collapses from ~10 reads per domain to 1. Reflect's "for every claim, read the canonical source" check still has to do per-claim verification (that's the *work*), but the broad scan that comes first is one call per domain instead of four.

**Note**: this is also the building block for a future `cross_domain_summary(role, [domains])` that returns a list of these — but starting with the single-domain RPC keeps the result shape predictable and lets consumers parallelize on their side.

---

### 6. `recent_observations`

**Today** (`reflect.md` §3d, `foresight.md` Memory Files, `housekeeping.md` cluster checks):
> "Gather observations — Read all `memory/*/observations.md` and `memory/*/*/observations.md` files. Filter to last 7 days. Cluster by domain. Cluster by topic."
> "Recent observations across all domains (last 7 days)."

**Round-trips today**: N reads + client-side date parse + cluster.

**Proposed RPC**: `recent_observations(role, since?, by_tag?, domain?)`

> Scope param naming: `domain` is canonical (consistent with open_actions, cluster_check, domain_summary, entity_audit, l0index). `by_domain` was the original name and is retained as a **DEPRECATED alias until 2026-07-12** — its lone divergence from the sibling RPCs was the muscle-memory trap behind PR #21. Note `by_domain` also names an *output* aggregate map below; that output field is unaffected.

**Result envelope**:
```json
{
  "since": "2026-05-23",
  "entries": [
    {"domain": "personal", "path": "personal/observations.md", "line": 41,
     "date": "2026-05-28", "tags": ["health"], "text": "..."},
    ...
  ],
  "by_domain": {"personal": 7, "work/acme": 4, ...},
  "by_tag": {"health": 5, "milestone": 3, ...}
}
```

Pre-computing `by_domain` / `by_tag` aggregates is the bit that matters — reflect's §3d clustering check is exactly this question, and right now every run re-walks every observation file to answer it.

**RBAC**: per-path, observation entries from unreadable domains filtered.

**Win**: the agent's "is there a synthesis opportunity?" check becomes one call. Same shape works for foresight's "what's happening this week?" scan.

---

### 7. `entity_audit`

**Today** (`housekeeping.md` §5b + `reflect.md` §3b):
> "Scan all `entities.md` files for registry format compliance: 3-line max ... Glacier candidates (status:inactive or last:>6 months) ... Missing metadata"
> "Scan all `entities.md` files for `(until YYYY-MM)` markers with past dates"

**Round-trips today**: N reads × deterministic regex over each entry block.

**Proposed RPC**: `entity_audit(role)`

**Result envelope**:
```json
{
  "format_violations": [
    {"path": "personal/entities.md", "name": "Jane", "lines": 7,
     "issue": "exceeds_3_line_compact", "has_detail_file": false}
  ],
  "glacier_candidates": [
    {"path": "...", "name": "...", "status": "inactive", "last": "2025-10-12",
     "age_days": 230}
  ],
  "missing_metadata": [
    {"path": "...", "name": "...", "missing": ["status", "last"]}
  ],
  "temporal_violations": [
    {"path": "...", "name": "...", "line": 23,
     "text": "(until 2025-12) — VP of platform",
     "needs": "strikethrough"}
  ]
}
```

**RBAC**: per-path.

**Win**: housekeeping's §5b + §5c become one call. The LLM still decides what to do (compress, glacier, flag for user review) — the daemon just enumerates violations.

---

### 8. `cluster_check`

**Today** (`reflect.md` §3 consolidation + §3c thread detection + §3d synthesis opportunities):
> "Scan all `observations.md` files and `cog-meta/self-observations.md` for clusters of 3+ entries on the same theme/tag."
> "Scan observations for topics that appear across 3+ dates or span 2+ weeks. These are thread candidates."
> "Cluster by topic — Group filtered entries by recurring keywords, tags, or subjects."

**Round-trips today**: N reads + N×M client-side clustering. The trigger thresholds ("3+ entries", "5+ observations in 7 days") are baked into the prompt.

**Proposed RPC**: `cluster_check(role, min_cluster_size?, since?, span_days?)`

**Result envelope**:
```json
{
  "by_tag": [
    {"tag": "health", "count": 6, "spans_days": 18,
     "domains": ["personal"],
     "samples": [{"date": "...", "text": "..."}, ...]}
  ],
  "by_keyword": [
    {"keyword": "kanban DB", "count": 5, "spans_days": 3,
     "domains": ["projects/chapterhouse", "infra"],
     "samples": [...]}
  ],
  "thread_candidates": [
    {"topic": "health/glp-1", "fragment_count": 4, "date_range": "2026-04-12..2026-05-29"}
  ]
}
```

Keyword extraction stays naive (substring frequency above a threshold) — this is the daemon, not an LLM. Sophisticated topic detection is the consumer's job *after* the cheap filter.

**RBAC**: per-path on the source observations.

**Win**: reflect §3 + §3c + §3d collapse from "read everything, filter to 7 days, group three different ways, dedup across §3c and §3d" into one call with the windows + thresholds passed as params. The agent's job becomes deciding which clusters to actually act on, which is where the judgment lives.

---

### 9. `briefing_bridge_compute`

**Today** (`housekeeping.md` §7):
> "Write key findings to `memory/cog-meta/briefing-bridge.md` so foresight can pick them up."

The bridge content is derived from §1–§5 above (stale items, birthdays, dormant domains, health escalations). All of those are now covered by `housekeeping_scan` + `entity_audit`. So `briefing_bridge_compute` would mostly be a *composer* — take the scan results and synthesize the markdown — which is exactly the kind of step that wants the LLM, not the daemon.

**Recommendation**: don't add this as an RPC. The composition is value-add; the inputs to it are now single calls. Skip.

---

### 10. `scenario_check`

**Today** (`reflect.md` §3e):
> "Scan `memory/cog-meta/scenarios/` for active scenario files. For each scenario where today >= `check-by` date: read the scenario, check assumptions, etc."

**Round-trips today**: 1 list + N reads + N×M assumption verifications.

**Proposed RPC**: `scenario_check(role)`

**Result envelope**:
```json
{
  "scenarios": [
    {"path": "cog-meta/scenarios/microsoft-stay-vs-leave.md",
     "check_by": "2026-06-15", "status": "due_now" | "overdue" | "active",
     "days_until_check": 17}
  ]
}
```

Just the schedule — assumption-verification is read-and-reason work that stays with the LLM.

**RBAC**: read on `cog-meta/scenarios/` path.

**Win**: small one. Worth shipping only if the broader pipeline is already getting RPC'd; on its own it's not the cost driver. Mention but don't prioritize.

---

## Suggested Implementation Order

By payoff (round-trips eliminated × frequency × consumer pain):

1. **`session_brief`** — fires every conversation, all consumers, two-file convention is the most-copied pattern in the ecosystem. Highest leverage.
2. **`housekeeping_scan`** — collapses housekeeping's entire §0–§2 orientation into one call. Housekeeping runs daily for serious users.
3. **`domain_summary`** — building block for both reflect and foresight; once it lands, the rest of those skills get cheap fast.
4. **`recent_observations`** — direct dependency for foresight and reflect's clustering. Pairs naturally with `domain_summary`.
5. **`entity_audit`** — concrete, bounded, well-specified. Easy to ship and worth real value to housekeeping.
6. **`cluster_check`** — the highest-conceptual-value one (replaces a ton of client-side regex) but also the most opinionated about thresholds. Land after the simpler ones are proven.
7. **`link_index_compute`** + **`link_audit`** — useful but housekeeping is the only consumer; relatively rare runs.
8. **`glacier_index_compute`** — small win, low frequency.
9. **`scenario_check`** — nice-to-have.

Skip: `briefing_bridge_compute` (composition belongs in the LLM).

## Cross-Cutting Concerns

### RBAC discipline

Every RPC above filters its result by what the calling role can read. The pattern from `open_actions` (collect-then-filter at the handler, store stays role-agnostic) is the right shape for all of them. Domain controller's `ValidateWrite` / `DomainForPath` makes this cheaper because we don't have to re-derive domain from path.

### Result-envelope conventions

A few rules to keep these consistent so consumers don't have to learn ten different shapes:

- Empty result is `[]` or `{}` at every layer, never `null`. (`open_actions` precedent.)
- Counts go in companion fields (`by_domain`, `by_tag`) — don't make consumers re-aggregate what we just walked.
- `since` params accept ISO date strings; default windows are documented per-RPC.
- All paths in results are forward-slash, never OS-native. Consumers parse paths constantly; one format only.
- All RPCs return `CodeInvalidParams` on malformed input, `CodeRBACDenied` on permission, `CodeStoreError` on I/O. No silent empty results from auth failure.

### Caching

None of these RPCs need a cache layer for v1 — the daemon's file walks are cheap on the scales we're talking about (low hundreds of files). If `housekeeping_scan` becomes a hot path in some future ClawPilot polling loop, that's the time to add an mtime-keyed cache, not before.

### Generalized `query` RPC — explicitly NOT recommended

Tempting to design one `query(selector)` RPC instead of ten typed ones. Resisted for the same reason GraphQL-everywhere is a mistake: typed surfaces let consumers code against a contract; a generalized selector pushes the contract into a runtime grammar that every consumer has to learn and the daemon has to validate. Typed RPCs are dumber and better.

## Open Questions

1. **Snapshot semantics**: Should `domain_summary` and friends return a `generation` field (cog-controller's mtime watermark) so consumers can detect "the world changed under me" between calls? Cheap to add. Defer to first real consumer.

2. **Write-companion RPCs**: `link_index_compute` returns the data; should there be a paired `link_index_write` that computes-and-writes atomically? Same question for `glacier_index_compute`. The current pattern (compute as RPC, write via existing `write` call) is fine — the agent can still inspect the result before deciding to persist. Probably skip the write-companions.

3. **Trigger thresholds**: `cluster_check` and `housekeeping_scan` bake threshold defaults into the daemon (50 obs, 10 completed actions, 5 cluster size). Should these be configurable per-instance via `config.yml`? Probably yes, as a follow-up — but the defaults from cog-prime are well-tested; ship with them as constants and add config later if real consumers diverge.

## Cards to file (one per shipped RPC)

Land this doc first. Each RPC above becomes its own implementation card after Brian reviews. Implementation cards reference this doc by section number.
