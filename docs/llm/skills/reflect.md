---
name: reflect
description: Mine recent activity for patterns and consolidate memory (3-gate pipeline)
triggers: [reflect, consolidate, patterns]
---

<!-- Canonical playbook — SSOT in cogmemory docs/llm/skills/. Hosts vendor this file and apply HOST-ADAPTATION blocks. Reference host implementation: ytsejam server/skills/. -->

# Reflect

Self-reflection and memory consolidation. Past-facing — mines interactions, fixes contradictions, distills patterns.

**Take your time.** This is a deep session. Read broadly, cross-reference, and ACT on findings. You're the maintainer of the knowledge base.

## Memory Access

All memory operations go through the cog tools (`cog_read`, `cog_write`, `cog_append`, `cog_patch`, `cog_outline`, `cog_search`, `cog_list`, `cog_move`) and `cog_rpc` envelopes. Paths are relative to the memory root (e.g. `cog-meta/patterns.md`, `projects/foo/observations.md`). Always address files by their domain **path**, never by domain id.

Session transcripts are NOT in memory — they live wherever the host stores its conversation logs and are read with the host's local (non-cog) `ls`/`grep`/`read`/`bash` tools.

> **HOST-ADAPTATION**: The host must expose its conversation transcripts to this skill via its local file tools. (reference host: ytsejam stores them in its data dir under `sessions/--chat--/*.jsonl` — yes, double dashes; the harness encodes the session cwd into the directory name.)

**Write discipline:**
- `cog_append` for observations and self-observations (additive entries)
- `cog_patch` for surgical edits to `patterns.md` / `improvements.md`
- `cog_write` only for full rewrites (hot-memory triage, the reflect cursor)

## Orientation (run first)

Scope your work before reading files:

1. `cog_rpc("session_brief")` — returns hot-memory, `patterns` (the cog-meta `patterns.md` body), the full `domains` list with paths, and per-domain action counts. This is your L0 routing sweep; no separate patterns read needed.
2. `cog_rpc("housekeeping_scan")` — fields: `since`, `changed_recently[]`, `thresholds{observations_over_cap[], completed_actions_over_cap, improvements_implemented_over_cap, hot_memory_over_cap, patterns_over_cap}`, `dormant_domains[]`, `stale_action_items[]`.
   - **`changed_recently[]`** is your focus list — concentrate the pass on these files, skip unchanged ones.
   - **`thresholds.observations_over_cap[]`** (each entry includes `by_primary_tag`) scopes Step 3 consolidation — these are the observation files approaching the archival threshold (50 entries).
3. Direct reads of reflect's own continuity files (no envelope carries them):
   - `cog_read("cog-meta/reflect-cursor.md")` — transcript-ingestion cursor (last processed session/timestamp)
   - `cog_read("cog-meta/self-observations.md")` — read-then-append target
   - `cog_read("cog-meta/improvements.md")` — read-then-triage target

### Minimum Data Check

Before proceeding, verify there's enough material to work with. Use `cog_rpc("recent_observations")` for counts:
- If total observations across all domains < 5: stop. Say "Not enough data yet. Keep capturing observations and run again when you have more material."
- If `changed_recently[]` is empty over the last 7 days: flag it. "Memory hasn't been updated recently. Consider capturing some observations first."
- If `patterns` (from `session_brief`) is empty and observations < 10: say "Too early to consolidate. You need ~10+ observations before patterns emerge."

Don't produce low-quality output from insufficient data. It's better to say "not yet" than to force weak patterns.

## Files in Hand After Orientation

- `cog-meta/self-observations.md` (direct read)
- `cog-meta/patterns.md` (via `session_brief.patterns`)
- `cog-meta/improvements.md` (direct read)
- `cog-meta/reflect-cursor.md` (direct read)
- Domain `observations.md`, `action-items.md`, `hot-memory.md` files: do NOT fan-out read them all. Read only the ones surfaced by `changed_recently[]`, `thresholds.observations_over_cap[]`, or needed for a specific verification below.

## Process

### 1. Review Recent Interactions

Mine the host's conversation transcripts since the cursor — use the host's local (non-cog) `ls`/`grep`/`read`/`bash` tools, NOT cog tools.

- Use the cursor from `cog-meta/reflect-cursor.md` to bound the work: only process sessions newer than the cursor.
- List the host's transcript directory, sort by date, read the new transcripts.

> **HOST-ADAPTATION**: The host must provide the transcript location, the chronological ordering convention, and the record format for mining. (reference host: ytsejam stores JSONL transcripts under `sessions/--chat--/*.jsonl` in its data dir — the directory really is named `--chat--`; the sibling `--subagent--/` holds background-worker transcripts — skip it by default, worker reports are injected back into the parent chat as `[Task ...]` messages. Filenames sort chronologically (`<ISO-timestamp>_<session-id>.jsonl`). Each line is a typed record, NOT a flat message — filter on the `.type` discriminator first (`session`, `model_change`, `message`, ...) or you'll choke on non-message rows. Messages nest under `.message`, and `content` is an array of typed parts. Working extraction filters:
>
> ```bash
> # user messages
> jq -r 'select(.type=="message" and .message.role=="user") | .message.content[]? | select(.type=="text") | .text' <file>
> # assistant text
> jq -r 'select(.type=="message" and .message.role=="assistant") | .message.content[]? | select(.type=="text") | .text' <file>
> ```
>
> `.role` at the top level returns nothing — it lives at `.message.role`.)

Look for:
- **Unresolved threads** — questions asked but never answered
- **Broken promises** — "I'll do X" that never happened
- **Repeated friction** — same question asked multiple ways, user corrections
- **Missed cues** — things the user had to repeat
- **Memory gaps** — information discussed but never saved
- **Feature ideas** — improvements that came up organically

At the end of the run (Step 6), advance the cursor with `cog_write("cog-meta/reflect-cursor.md", ...)`.

### 2. Consistency Sweep

Systematic contradiction detection:

1. **Hot-memory vs canonical sources**: For every claim in a hot-memory file, verify against the canonical file (`cog_read` the cited source — you decide which file is canonical for which claim). Fix hot-memory if stale (`cog_write` for full triage rewrites).
2. **Cross-file fact check**: Verify shared facts are consistent. More recent source wins.
3. **Temporal validity check**: `cog_rpc("entity_audit")` returns `temporal_violations` — `(since YYYY-MM)` markers >6 months old (flag for review) and `(until YYYY-MM)` past dates (add strikethrough via `cog_patch`). No manual entity scanning needed.
4. **Cross-domain entity check**: the same `entity_audit` envelope enumerates entries per path — same person in multiple entity files → keep one canonical, convert others to pointers (`cog_patch`). You decide which duplicates are legitimately domain-scoped vs. genuine drift.

### 3. Consolidation (Condition Pipeline)

Rigorous observation → pattern promotion. Three gates prevent noise from entering pattern files.

**Gate 1: Cluster Detection**

`cog_rpc("cluster_check", {"min_cluster_size": 3, "since": "7d"})` replaces manual observation grepping — it returns tag clusters, keyword clusters, and thread candidates in one envelope. A cluster is promotable when ALL conditions are met:
- ≥3 entries with the same primary tag
- Entries span ≥7 days (not a single-day burst)
- ≥3 distinct dates (not the same insight repeated on one day)
- Tag is specific (reject broad tags: "work", "home", "general", "misc")

The RPC surfaces what's clustering; whether a cluster passes the span/date/specificity bar — and Gates 2 and 3 entirely — is your judgment.

**Gate 2: Coverage Check**

Before promoting, check if the pattern ALREADY EXISTS:
- Check `cog-meta/patterns.md` (already in hand from `session_brief`) and any domain satellite `patterns.md` (`cog_read`)
- If an existing pattern already covers this cluster's insight → skip (not a gap)
- If the new insight SUBSUMES an existing pattern (broader, more accurate) → plan to REPLACE the old one

**Gate 3: Synthesis & Write**

For each uncovered cluster:
- Distill into one actionable, timeless pattern line
- Style-match against existing patterns (same voice, same structure)
- Add `<!-- promoted:YYYY-MM-DD theme:tag -->` audit trail at the end of the line
- `cog_patch` it into `cog-meta/patterns.md` (universal) or `{domain-path}/patterns.md` (domain-specific) — edit the relevant section or add the new bullet
- If replacing an existing pattern, `cog_patch` the old line into the new one

**Replacement is healthy** — patterns evolve. A new pattern that subsumes 2 older ones should replace both. Track replacements in debrief.

**Pattern file caps:**
- Core `patterns.md`: hard limit 70 lines / 5.5KB — universal rules only
- Satellite files: soft cap 30 lines each
- `housekeeping_scan.thresholds.patterns_over_cap` flags files near the cap. If near cap: merge overlapping rules or replace weaker patterns (`cog_patch`). Never just truncate.

**Spike Detection (below promotion bar):**

Clusters with ≥5 entries in <7 days don't meet the 7-day span requirement. But they signal a heating topic:
- Note in debrief as "Spike: [tag] — [N] entries in [N] days"
- These are thread-raising candidates (see Step 5)

**Hot-memory relevance:**
- **Promote**: Pattern heating up → add to hot-memory
- **Demote**: Item gone quiet (no references 2+ weeks) → remove from hot-memory
- Hot-memory triage is a full rewrite: `cog_write` the file (the `hot_memory_over_cap` threshold from `housekeeping_scan` tells you when triage is overdue)

### 4. Entity Format Enforcement

`cog_rpc("entity_audit")` replaces the per-file entity scans. Act on its findings:
1. **3-line check**: `format_violations` (entries >3 lines) → compress in place (`cog_patch`) or flag for thread promotion
2. **Status/last fields**: `missing_metadata` — every entry needs `status:` and `last:` fields; fill via `cog_patch`
3. **Cross-domain pointers**: Same person in multiple files → one canonical, others `see [[link]]`

Fixing is judgment: do NOT auto-fix health or family-sensitive facts — flag those for user review instead.

### 5. Thread Candidate Detection

The `cluster_check` envelope from Step 3 already includes thread candidates — topics appearing across 3+ dates or spanning 2+ weeks. For each:
- Check if thread already exists
- If not: "Thread candidate: [topic] — [N] fragments across [date range]"
- Don't auto-create — suggest

### 6. Act on Findings

**Write:**
- New self-observations → `cog_append` to `cog-meta/self-observations.md` (max 5 per run — merge lower-signal ones)
- Pattern updates → `cog_patch` on `cog-meta/patterns.md`
- Improvement ideas → `cog_patch` on `cog-meta/improvements.md`
- Memory gaps → write to the appropriate domain files by PATH (e.g. `cog_append("projects/foo/observations.md", ...)`)
- Advance the transcript cursor → `cog_write("cog-meta/reflect-cursor.md", ...)` with the last processed session/timestamp

**Connect:**
- Scattered information → add `[[links]]`
- When adding A→B, check if B benefits from `[[A]]` back

### 7. Debrief

Compose a summary:
- *What I learned* — new patterns and insights
- *What I fixed* — memory gaps filled, corrections made
- *What to watch* — things to be mindful of
- *Thread candidates* — topics worth raising

**List every file modified and summarize changes.** Never just say "Done".

## Artifact Formats

- **Self-observation**: `- YYYY-MM-DD [tag]: <observation>`
- **Pattern**: Edit existing section or add new bullet
- **Improvement**: `- <idea> (added YYYY-MM-DD)`
