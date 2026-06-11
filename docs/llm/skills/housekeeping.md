---
name: housekeeping
description: Memory maintenance — archive, prune, temporal sweep, rebuild indexes
triggers: [housekeeping, maintenance, archive, prune]
---

<!-- Canonical playbook — SSOT in cogmemory docs/llm/skills/. Hosts vendor this file and apply HOST-ADAPTATION blocks. Reference host implementation: ytsejam server/skills/. -->

# Housekeeping

Memory maintenance — archive, prune, index, enforce format. The janitor.

## Memory Access

All paths are relative to the memory root. Use the `cog_*` tools for reads
and writes; use `cog_rpc` for the structured envelopes. Role is injected
automatically — never pass it.

## Orientation (run first)

One call scopes the entire pass:

```
cog_rpc("housekeeping_scan")
```

The envelope returns:

- `since` — when the last pass ran
- `changed_recently[]` — files changed since then; this bounds every
  subsequent sweep (skip files not in this set unless the step is a full
  rebuild)
- `thresholds.observations_over_cap[]` — observations files >50 entries,
  each with `by_primary_tag` counts (the archival routing comes pre-bucketed)
- `thresholds.completed_actions_over_cap[]` — action-items files >10
  completed items
- `thresholds.improvements_implemented_over_cap[]` — improvements files >10
  implemented items
- `thresholds.hot_memory_over_cap[]` — hot-memory files over the 50-line cap
- `thresholds.patterns_over_cap[]` — pattern files over the dual cap
  (lines and size)
- `dormant_domains[]` — domains with 0 observations in >4 weeks
- `stale_action_items[]` — open items with `age_days`

Only work files the envelope flags. Skip everything else.

### Minimum Data Check

Before proceeding, verify there's enough material to maintain:
- If no observations files exist or all are empty: stop. Say "Nothing to maintain yet. Start by capturing some observations and the system will grow."
- If all entry counts are well below thresholds (obs < 10, items < 3): say "Memory is still light. No maintenance needed yet — keep building."

Don't run a full pipeline over an empty system. Acknowledge and exit early.

## Process

### 1. Garbage Collect

Archive stale data per glacier rules. All glacier files need YAML frontmatter.

The orientation envelope already did the counting — drive every archival
decision off `thresholds.*`:

**Observations — archive by primary tag:**
- For each file in `thresholds.observations_over_cap[]` (>50 entries), use
  its `by_primary_tag` buckets as the group key. For each tag bucket, pick
  the oldest entries (judgment call when timestamps are irregular) and move
  them to `glacier/{domain-path}/observations-{tag}.md`.

**Other files:**
- Each file in `thresholds.completed_actions_over_cap[]` (>10 completed) →
  `glacier/{domain-path}/action-items-done.md`
- Each file in `thresholds.improvements_implemented_over_cap[]` →
  `glacier/{domain-path}/improvements-done-{YYYY}.md`
- Entities inactive 6+ months (see §7's `entity_audit` glacier candidates) →
  `glacier/{domain-path}/entities-inactive.md`

**Mechanics for every archival move:**
1. Compose the glacier slab yourself — YAML frontmatter with `type`,
   `domain`, `tags`, `date_range`, `entries`, `summary` — followed by the
   archived entries.
2. `cog_write("glacier/{domain-path}/...", content)` to create the slab
   (or `cog_append` if the slab already exists).
3. `cog_patch(source_path, old_text, new_text)` to remove the archived
   entries from the source file. Never leave them duplicated.

### 2. Prune Hot Memory

Keep ALL `hot-memory.md` files under 50 lines.

`thresholds.hot_memory_over_cap[]` names the offenders — don't re-check the
others. For each flagged domain, fetch the current body with:

```
cog_rpc("domain_summary", {"domain": "<domain>"})
```

The `hot_memory` field in the response is the body to trim. Apply the cuts
with `cog_patch` (or `cog_write` for a heavy rewrite).

**Pruning priority:**
1. Resolved items (strikethrough, "DONE", "RESOLVED")
2. Past events (dates already occurred)
3. SSOT violations (same fact in hot-memory AND canonical file)
4. Stale entries (not referenced 14+ days)
5. Low-signal entries (FYI with no action or deadline)

SSOT-violation detection means comparing hot-memory lines against the
canonical file's content — `cog_read` the canonical file when you need to
verify; no envelope does that comparison for you.

**Where trimmed entries go:**
- Lasting value → `cog_append` to the domain's `observations.md`
- Purely historical → let them go
- Never silently delete — move or note in debrief

### 3. Surface Opportunities

The envelope already did the scanning:

- **Stale items** (open >2 weeks): straight from `stale_action_items[]` —
  list each with its `age_days` and a suggested action
- **Dormant domains** (0 observations in >4 weeks): straight from
  `dormant_domains[]` — flag each
- **Health escalation** (open >6 months): same `stale_action_items[]`
  rows, filtered to `age_days > 180` — flag with urgency
- **Birthday prep** (<2 weeks away): there is no birthday RPC. Call
  `cog_rpc("domain_summary", {"domain": "personal"})` for orientation, then
  `cog_read` the personal calendar/entities files yourself, scan for
  upcoming birthdays, pull interests, and suggest ideas.

### 4. Temporal Validity Sweep

Two sources cover the sweep:

1. **Entities**: `cog_rpc("entity_audit")` returns `temporal_violations[]`
   — expired temporal markers in entity files, found for you.
2. **Everything else** (hot-memory, action-items, threads): scan yourself,
   bounded by `changed_recently[]` — `cog_read` each candidate and look for
   `<!-- until:YYYY-MM-DD -->` and `<!-- until:YYYY-MM-DD grace:N -->`
   markers.

Then apply the rules:

1. **Compute expiry**: `until_date + grace_days` (grace defaults to 0)
2. **If expired**: remove the line from its current file via `cog_patch`
   - If the line has lasting value → `cog_append` to `observations.md` with
     `[archived]` tag
   - If purely temporal (event countdown, temporary state) → discard
3. **If expiring within 7 days**: leave in place but add to debrief as
   "expiring soon"

This is deterministic — no judgment needed. Date math only.

**Do NOT touch `<!-- from:YYYY-MM-DD -->` markers** — those are stable-since markers and never expire.

### 5. Rebuild Indexes (Deterministic)

Rebuild ALL indexes from source of truth — no LLM judgment, pure data extraction. The daemon computes; you render and write.

**5a. Glacier Index**

```
cog_rpc("glacier_index_compute")
```

returns the full entries array (`path, domain, type, tags, date_range,
entries, summary`). Render the table and `cog_write("glacier/index.md", ...)`:

```markdown
# Glacier Index
<!-- Auto-generated. Do not edit. -->
<!-- Last updated: YYYY-MM-DD -->

| File | Domain | Type | Tags | Date Range | Entries | Summary |
|------|--------|------|------|------------|---------|---------|
```

**5b. Domain INDEX.md files**

For each domain (from `session_brief` or `cog_rpc("domains.list")`), fetch
the per-file L0 headers:

```
cog_rpc("l0index", {"domain": "<domain>"})
```

Render the table and `cog_write("{domain-path}/INDEX.md", ...)`:

```markdown
# {Domain} Index
<!-- Auto-generated from L0 headers. Do not edit. -->
<!-- Last updated: YYYY-MM-DD -->

| File | Summary |
|------|---------|
| hot-memory.md | Current state and priorities |
| observations.md | Timestamped events and learnings |
```

**CRITICAL: write to the domain's *path* (e.g. `projects/chapterhouse/INDEX.md`), NEVER the id — the daemon rejects id-as-path writes.** Every row in the table is `{domain-path}/{file}.md` territory too.

**Key principle**: These indexes are DETERMINISTIC — computed from L0 headers that already exist. If a file has no L0 header, list it with summary "(no L0 header — needs one)". Never invent summaries; just reflect what's there.

This prevents index drift — the failure mode where LLM-generated indexes silently go stale because the generation step was skipped or failed.

### 6. Link Audit

```
cog_rpc("link_audit")
```

returns link candidates (`source_path, line, entity_name, target_link,
context`) covering the original sweep:

1. Entity mentions matching `### Name` headers → add `[[links]]` if missing
2. Cross-domain references → add cross-domain links
3. Action item references → link observations to tasks

The candidate set is suggestions, not auto-edits — decide which references
are substantive enough to link, then apply each accepted one with
`cog_patch`.

### 7. Entity Format Enforcement

```
cog_rpc("entity_audit")
```

returns format violations, glacier candidates, and missing-metadata flags
across all `entities.md` files in one envelope. Apply the fixes with
`cog_patch`:

1. **3-line max**: Entries >3 lines → compress or flag for thread promotion
2. **Glacier candidates**: Inactive >6 months → move to glacier (leave stub)
   — slab via `cog_write` per §1 mechanics, stub via `cog_patch`
3. **Missing metadata**: Flag entries without `status:` or `last:` fields

### 8. Rebuild Link Index

```
cog_rpc("link_index_compute")
```

returns the reverse index (target → sources) for all `[[wiki-links]]`
outside glacier. Render and `cog_write("link-index.md", ...)`:

```markdown
# Memory Link Index
<!-- Auto-generated. Do not edit. -->
<!-- Last updated: YYYY-MM-DD -->

| Target | Linked from |
|--------|-------------|
```

### 9. L0 Header & Format Hygiene

**Detection is global, not bounded.** Diff `cog_list()` against the
`l0index` output per domain: every `.md` file that appears in the listing
but not in `cog_rpc("l0index", {"domain": ...})` is missing its
`<!-- L0: ... -->` header (the daemon's l0index silently omits headerless
files — absence from it IS the signal). Skip `glacier/` (read-only).

**Repair every missing header this run — don't defer to "when the file is
next touched"; dormant files never get touched.** Use `changed_recently[]`
only to order the work, not to limit it. For each missing file:

1. `cog_read(path)`, write a one-line summary (max 80 chars)
2. `cog_patch` to insert the header as line 1 (before the title)

If any headers were added, re-run `l0index` for the affected domains and
rewrite their `INDEX.md` (§5b) so the index reflects the repair.

**Observation format normalization**: while sweeping, if any
`*observations.md` file contains legacy non-conforming blocks (e.g.
`## date — title` headings with plain bullets above strict-format lines),
normalize them: convert each legacy bullet to
`- YYYY-MM-DD [tags]: text` (date from the enclosing heading, tags
inferred, content preserved verbatim), then `cog_write` the full
consistent file. The daemon validates appends against the strict format,
so mixed files mean every future `cog_append` succeeds but the file reads
inconsistently — fix the legacy block, never loosen the new entries.

### 10. Debrief

Summarize:
- What was archived/pruned
- Upcoming events flagged
- Action items surfaced
- Links added
- Files modified (list each one)
