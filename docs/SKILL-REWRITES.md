# SKILL-REWRITES — cog-prime command rewrites against the consolidated RPC vocabulary

This doc translates cog-prime's `.claude/commands/*.md` skill bodies into the
new cogmemory RPC vocabulary defined in [RPC-CONSOLIDATION.md](./RPC-CONSOLIDATION.md).

Each section follows the same six-part shape:

1. Original orientation block (shell scans / file reads — quoted verbatim)
2. Rewritten orientation block (one or two RPC calls; named fields drive
   downstream decisions)
3. Original process steps that involve memory reads (quoted)
4. Rewritten process steps (RPCs where they fit; prose where no RPC exists —
   writes and scenario assumption-verification stay prose by design)
5. LLM-judgment-preserved callout (what intentionally stays with the model)
6. Round-trip delta (current per-file shell+read count vs rewritten RPC count)

Guardrails for every section:

- **No new RPCs.** If a step needs something the vocabulary does not provide,
  the gap is logged in the `Gaps surfaced` section at the bottom of this doc
  rather than papered over with a hypothetical call.
- **Write paths are not rewritten.** Appends/patches to canonical files are
  already single calls (`cog_append`, `cog_patch`, `cog_write`); they're
  enumerated in prose where relevant.
- **Skill meaning is preserved.** The rewrites change how memory is *fetched*,
  not what the skill is *for*.

---

## reflect

Source: <https://github.com/marciopuga/cog/blob/main/.claude/commands/reflect.md>

Reflect is the deep self-improvement pass: ingest recent session transcripts,
sweep memory for contradictions, run consolidation/relevance checks, enforce
entity-registry format, detect thread candidates, surface synthesis
opportunities, run the scenario feedback loop, and write back self-observations
+ patterns + improvements. It is the single heaviest memory-touching command
in cog-prime.

### 1. Original orientation block

> ```bash
> # What changed since last run? Focus here.
> find memory/ -type f -name "*.md" -mtime -1 | sort
>
> # L0 summaries for all domains — quick routing without opening INDEX.md files
> grep -rn "<!-- L0:" memory/ --include="*.md" | grep -v glacier/ | sort
>
> # Entry counts for files approaching archival threshold
> grep -c "^- " memory/cog-meta/self-observations.md memory/personal/observations.md memory/*/observations.md memory/*/*/observations.md 2>/dev/null
> ```
>
> Focus on recently-changed files. Skip files that haven't been modified since last run.

Followed by activation reads of `memory/cog-meta/reflect-cursor.md`,
`self-observations.md`, `patterns.md`, `improvements.md`, then
`memory/domains.yml` plus every domain's `observations.md`, `action-items.md`,
and `hot-memory.md` "as needed."

### 2. Rewritten orientation block

Two RPCs cover the whole orient pass:

- `session_brief({ window: "since_last_reflect" })` — returns the L0 index
  table, per-file `last_modified` timestamps, observation counts per
  `observations.md`, current `hot-memory.md` snippets, and `reflect-cursor`
  state. Drives **which domains are in scope this pass** (anything where
  `last_modified > reflect_cursor.last_processed`) and **which observation
  files are near the archival cap** (sort by `entries` descending; flag any
  ≥ archival threshold from `housekeeping_scan` for downstream step 3).
- `recent_observations({ since: reflect_cursor.last_processed, group_by:
  ["domain", "tag"] })` — pre-bucketed feed of new observations. Drives the
  consolidation cluster check in step 3 (a domain or tag with 3+ new entries
  becomes a pattern-distill candidate; 5+ in 7 days becomes a synthesis
  suggestion in step 3d).

If `session_brief.reflect_cursor.last_processed == "never"`, fall back to the
prose instruction: read the last 3 Claude Code session JSONLs from
`session_path` directly — session transcripts are not in cogmemory's index.

### 3. Original process steps (memory reads quoted)

> **§2 Cross-Reference Memory & Consistency Sweep**
>
> 1. Hot-memory vs canonical sources: Read each domain's `hot-memory.md`. For
>    every factual claim, read the canonical source file and verify.
> 2. Cross-file fact check: Verify facts shared between files are consistent.
> 3. Temporal validity check: Scan all `entities.md` files for `(since
>    YYYY-MM)` >6 months and `(until YYYY-MM)` not yet ~~strikethrough~~.
> 5. Cross-domain entity check: If the same person appears in multiple
>    `entities.md` files across domains, check for fact duplication.

> **§3 Consolidation Check + Hot-Memory Relevance**
>
> Scan all `observations.md` files and `cog-meta/self-observations.md` for
> clusters of 3+ entries on the same theme/tag.
> Review all `hot-memory.md` files: promote heating patterns, demote quiet
> items.

> **§3b Entity Registry Format Enforcement**
>
> Scan all `entities.md` files for format compliance: 3-line check,
> status/last fields, cross-domain pointers.

> **§3c Detect Thread Candidates**
>
> Scan observations for topics that appear across 3+ dates or span 2+ weeks.

> **§3d Proactive Synthesis Suggestions**
>
> Gather observations — Read all `memory/*/observations.md` and
> `memory/*/*/observations.md` files. Filter to last 7 days. Cluster by domain
> and by topic.

> **§3e Scenario Feedback Loop**
>
> Scan `memory/cog-meta/scenarios/` for active scenario files. For each
> scenario where today >= `check-by` date: Read the scenario and its cited
> dependency files. Check: has the decision been made? Have assumptions
> broken?

### 4. Rewritten process steps

Step-by-step replacement; quoted file paths above map to single RPC calls.

- **§2.1–§2.2 hot-memory vs canonical + cross-file fact check** —
  `domain_summary({ domain: D, include: ["hot_memory_claims",
  "canonical_diffs"] })` per domain returned by orientation. Returned
  `canonical_diffs[]` already lists each claim in hot-memory that disagrees
  with the canonical source file plus the canonical value. Loop is one RPC
  per in-scope domain, not one file read per claim.
- **§2.3 + §3b entity temporal + format sweep** — `entity_audit({ checks:
  ["since_age", "until_strikethrough", "three_line", "status_last_fields",
  "cross_domain_duplication"] })`. Returns flagged-entry list with file +
  line + violation kind; health/family-sensitivity flag travels through the
  same envelope (skill still suppresses auto-fix on those).
- **§3 consolidation** — already half-done by `recent_observations` from
  orientation; the heavy "find clusters of 3+" is the dedicated call
  `cluster_check({ scope: "observations", min_cluster: 3 })`. Pattern
  distillation itself stays LLM-driven (see §5 below).
- **§3 hot-memory promote/demote** — driven from
  `domain_summary.hot_memory_activity` returned in the §2 sweep; no extra
  call needed. The skill still uses prose `cog_patch` for the actual edit.
- **§3c thread candidates** — `cluster_check({ scope: "observations",
  by: "topic", min_dates: 3, min_span_days: 14 })`. Returned `candidates[]`
  drives the "suggest, don't auto-create" output verbatim.
- **§3d synthesis suggestions** — `recent_observations({ since: "7d",
  group_by: ["domain", "topic"] })` (or reuse the §1 call if its window
  covered ≥7d). Trigger conditions (`domain ≥ 5` or `topic ≥ 5`) read
  straight off the grouped counts.
- **§3e scenario feedback** — `scenario_check({ status: "active",
  due_by_today: true })` returns each active scenario, its `check_by`
  date, and the dependency-file hashes recorded at scenario creation. The
  RPC handles "is the check due" and surfaces hash mismatches that *suggest*
  assumptions may have moved, but **verifying whether the assumption
  actually broke is an LLM judgment call against the cited file content**
  — keep that as prose: read the cited file, reason, write retrospective
  via `cog_patch`/`cog_append`.

Writes (§5 of the original — append self-observations, patch patterns,
append improvements, reorganize entities, update `reflect-cursor`) stay as
direct `cog_append` / `cog_patch` / `cog_write` calls. No RPC change.

### 5. LLM-judgment-preserved callout

The rewrite keeps the model on the hook for everything that is not a
mechanical fetch:

- Reading session transcripts and identifying unresolved threads, broken
  promises, repeated friction, missed cues, memory gaps, and feature ideas
  (§1 of the original). Transcripts live outside cogmemory; no RPC.
- Distilling observation clusters into timeless rule statements for
  `patterns.md` (RPC finds the clusters; phrasing the rule is judgment).
- Deciding pattern routing — core vs domain satellite, and whether the new
  rule is universal enough to deserve a core slot under the 70-line cap.
- Sensitivity gates on `entity_audit` output: health and family-sensitive
  facts are flagged-only, never auto-fixed.
- Scenario assumption-verification: deciding whether a dependency-file hash
  change actually invalidates the scenario or is incidental drift.
- Composing the debrief (`What I learned / fixed / want / to watch`).

### 6. Round-trip delta

Original orient + read passes (counting each `observations.md`, `hot-memory.md`,
`action-items.md`, `entities.md` per domain plus the cog-meta files, against
a representative ~6-domain memory tree):

- 3 shell scans (find / grep L0 / grep `^-` counts)
- 4 fixed cog-meta reads (reflect-cursor, self-observations, patterns, improvements)
- 1 `domains.yml` read
- ~6 × {observations, hot-memory, action-items, entities} = ~24 domain reads
  for the consistency + consolidation + entity sweeps
- ~6 × hot-memory + ~6 × observations re-reads during §3 cluster scans
- 1–3 session JSONL reads
- N scenario file reads in §3e (1 per active scenario; 2–6 typical)

**Original total: ~45–55 file/scan operations per pass.**

Rewritten:

- 1 `session_brief`
- 1 `recent_observations`
- 6 × `domain_summary` (1 per in-scope domain — orientation can shrink this
  to "only domains modified since cursor")
- 1 `entity_audit`
- 1 `cluster_check` (observations, min_cluster=3) — same call also returns
  the thread-candidate set when invoked with the topic/span params, but the
  rewrite uses two cheap calls to keep result envelopes small
- 1 `cluster_check` (topic / 3 dates / 14 days)
- 1 `scenario_check`
- 1–3 session JSONL reads (unchanged — out of cogmemory's scope)
- N file reads only for scenarios where `scenario_check` reports a hash
  mismatch worth verifying (typically 0–2)

**Rewritten total: ~13–18 RPC/read operations per pass.**

Round-trip delta: **≈ 3× fewer fetches, with the cluster + entity + scenario
sweep collapsed from N×file-read loops into bounded RPC calls.**

---

## Gaps surfaced

(Reserved for entries from any card in this doc whose skill needs a
capability the consolidated RPC vocabulary doesn't yet cover. Append below
in the form `- skill: gap — one-line shape of what's missing`.)
