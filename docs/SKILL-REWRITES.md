# Cog-Prime Skill Rewrites — Translation Table

Round-trip-by-round-trip translation of the cog-prime skill bodies
(https://github.com/marciopuga/cog/blob/main/.claude/commands/) into the
RPC vocabulary proposed in [RPC-CONSOLIDATION.md](./RPC-CONSOLIDATION.md).

One section per cog-prime command. Each section follows a fixed
six-part shape:

1. Original orientation block (shell scans / file reads, quoted)
2. Rewritten orientation block (1–2 RPC calls; named driving fields)
3. Original process steps that involve memory reads (quoted)
4. Rewritten process steps (RPCs where they exist; prose where they
   don't — writes and scenario-style assumption checks stay LLM)
5. LLM-judgment-preserved callout — what *must* stay with the model
6. Round-trip delta — current N vs rewritten N (range OK)

**Guardrails (apply to every section):**
- No new RPCs are proposed here. Anything the existing vocabulary
  can't cover goes into the "Gaps surfaced" section at the bottom.
- Write paths are not rewritten — they're already single calls.
- Skill *meaning* is preserved; only the data-access shape changes.
- Cog-prime's section structure is kept; this is a translation, not
  a redesign.

---

## `setup`

Source: https://github.com/marciopuga/cog/blob/main/.claude/commands/setup.md

### 1. Original orientation block

The skill has no explicit "read X first" preamble. Its orientation is
implicit and re-run-only: when invoked on an existing install it must
discover the current manifest and on-disk state before asking the user
what to add. In cog-prime that means an ad-hoc combination of:

> "Read `memory/domains.yml`" (to know what already exists)
> "List `~/.claude/projects/` and find the directory that matches this
> project's path" (Phase 3d, transcript discovery)
> Implicit `ls memory/{domain.path}/` to decide which starter files
> are missing (Phase 3b, "create … if it doesn't exist")
> Implicit `ls .claude/commands/` to decide which command files need
> (re)writing (Phase 3c)

Round-trips on a re-run: 1 YAML read + N directory listings + 1
transcripts dir listing ≈ 3–8 filesystem calls before the conversation
can resume.

### 2. Rewritten orientation block

A single RPC covers the manifest half of the discovery:

- `domain_summary(role="setup", domain="*")` — or, more cheaply,
  consume the `domains` slice already returned by
  `session_brief(role="setup")`. The driving field is the **list of
  `id`s already present**: it tells the skill whether this is a fresh
  install ("no domains yet → run full Phase 1") or a re-run ("ask
  'add more or reconfigure?'", Rule 5).

For each existing domain, the same `domain_summary` response carries
`files_present[]`, which drives Phase 3b's "create if missing" loop
without a per-domain `ls`.

Transcript-path discovery (`~/.claude/projects/<slug>`) has no RPC
analogue — it inspects the Claude Code install, not Cog memory — so it
stays as a one-shot filesystem call. Called out under "Gaps surfaced".

### 3. Original process steps involving memory reads

Setup is overwhelmingly a *write* skill. The few read-shaped steps are:

> Phase 3a: "Write the complete manifest file." — implicitly requires
> reading the prior `domains.yml` so cog-meta and untouched entries
> are preserved across re-runs.
>
> Phase 3b: "For each file in the domain's `files` array, create
> `memory/{domain.path}/{file}.md` **if it doesn't exist**." — requires
> a directory listing per domain.
>
> Phase 3c: "If the file already exists, overwrite it (the template is
> the source of truth)." — no read needed, but the loop is bounded by
> the manifest's domain list, which must first be in hand.
>
> Phase 3e: "Read `CLAUDE.md`. Find the domain routing table … Keep
> all non-domain rows … as-is" — single file read, no rewrite.

### 4. Rewritten process steps

| Phase | Original shape | Rewritten shape |
|---|---|---|
| 1 — Discovery (conversational) | unchanged | unchanged (LLM-driven dialogue) |
| 2 — Confirm summary | unchanged | unchanged (LLM-formatted prose) |
| 3a — Write `domains.yml` | read prior YAML + write new | `session_brief(role="setup")` provides current manifest in-band → render new YAML → single write call (no RPC; write path stays prose per guardrails) |
| 3b — Create starter files | per-domain `ls` + per-file create | for each domain, consume `files_present[]` from the same `session_brief`/`domain_summary` payload; write only the diff. Writes remain prose. |
| 3c — Generate command files | read template + write per domain | unchanged — template read is one-shot and command-file writes are unconditional ("overwrites the file") so no read is needed at all. |
| 3d — Discover session transcript path | `ls ~/.claude/projects/` + write `reflect-cursor.md` | unchanged — outside Cog memory; see "Gaps surfaced". |
| 3e — Update `CLAUDE.md` routing table | read CLAUDE.md + regenerate domain rows | unchanged — `CLAUDE.md` is a project file, not Cog memory; no RPC applies. |
| 4 — Summary | unchanged | unchanged. |

The net effect: the 3–8 startup reads collapse into a single
`session_brief` call, and the per-domain "does this file exist?" check
disappears entirely.

### 5. LLM-judgment-preserved callout

The whole *point* of `setup` is conversational synthesis — that work
stays with the model:

- **Domain inference from a free-form interview** (Phase 1): mapping
  "I run a side project called myapp, my wife and I have two kids,
  I'm a designer at Acme" into `personal` + `acme` (work) + `myapp`
  (side-project), plus customized `files` lists ("kids → add
  `school`", "health condition → add `health`").
- **Trigger keyword inference** for each domain (company names,
  project names, colleague names).
- **The Phase 2 confirmation rewrite** — restating the proposed
  manifest in plain English so the user can sanity-check before any
  file is touched.
- **Re-run reconciliation judgment** (Rule 5 + Rule 2): deciding
  whether the user wants to add a domain, rename one, or merge
  observations into a renamed path — the RPC can list what's there,
  but only the LLM can interpret intent.

### 6. Round-trip delta

- **Current**: ~3–8 filesystem round-trips at orientation on a re-run
  (1 `domains.yml` read + 1 transcripts dir listing + N per-domain
  `ls` calls for the "create if missing" loop + 1 `CLAUDE.md` read).
  Fresh installs are ~1–2 (transcripts listing + `CLAUDE.md` read).
- **Rewritten**: **1 RPC** (`session_brief`) + 1 unavoidable
  filesystem read each for the transcripts directory and `CLAUDE.md`,
  both of which sit outside Cog memory. Fresh installs are the same 1
  RPC + 2 reads.
- **Delta**: 3–8 → ~1 inside Cog memory; total round-trips drop to a
  flat **~3** regardless of install size.

This skill is, and stays, write-dominated; the rewrite's value is
small in absolute terms but eliminates the only place where setup's
cost scaled with the number of existing domains.

---

## `scenario`

Source: https://github.com/marciopuga/cog/blob/main/.claude/commands/scenario.md

Decision-modeling skill: take a decision point, branch into 2–3 paths, ground each in real memory data, map dependencies + timelines + canary signals, and write to `cog-meta/scenarios/{slug}.md`.

### 1. Original orientation block

From `## Memory Files`:

> Read based on scenario topic — this is focused, not a broad scan:
> - `memory/hot-memory.md` (cross-domain strategic context)
> - `memory/personal/calendar.md` (upcoming timeline for overlay)
> - `memory/personal/action-items.md` (existing commitments, constraints)
> - Work domain action-items (read `memory/domains.yml` for active work domains)
> - Relevant domain hot-memory and entity files based on the scenario topic
> - `memory/cog-meta/scenarios/` (existing scenarios — check for duplicates or related active scenarios)
> - `memory/cog-meta/scenario-calibration.md` (past accuracy — calibrate confidence accordingly)

Concrete shape on a 3-domain-topic with ~4 active scenarios: 1 hot-memory read + 2 personal reads (calendar, action-items) + 1 `domains.yml` read + ~3D domain-file reads (hot-memory, action-items, entities per topic-relevant domain) + 1 `cog-meta/scenarios/` listing + ~4 scenario-file reads to dedupe + 1 calibration read ≈ **~14 reads** before any branching work begins.

### 2. Rewritten orientation block

Two RPC calls cover the same ground:

1. `scenario_check(role)` → `{scenarios: [{path, check_by, status, days_until_check}, …]}`. The **`path`** + **`status`** fields drive dedupe ("is this decision already an active scenario?") and contingency framing ("is this related to one due now?"). Replaces the directory listing + per-file reads of `cog-meta/scenarios/`.
2. For each domain D the user's prompt touches, `domain_summary(role, D, since="7d")` → `{hot_memory, open_action_count, recent_observations, files_present, …}`. The **`hot_memory`** body + **`recent_observations`** array feed dependency mapping; `open_action_count` is the early-exit signal ("decision is already committed, don't scenario it"). Replaces hot-memory + action-items + entities reads per domain, plus the `domains.yml` lookup.

Two reads stay as direct file fetches (no RPC, called out per the guardrails):

- `cog-meta/scenario-calibration.md` — confidence-calibration source. Single file, low frequency. **Direct read.**
- `personal/calendar.md` — week-grain calendar overlay. **Direct read.**

The global `memory/hot-memory.md` drops out: the session-start `session_brief` call (run once per conversation) already returned it.

### 3. Original process steps involving memory reads

§2 Dependency Mapping:

> Read across memory files to identify what this decision depends on and what depends on it. **Upstream dependencies** (things that constrain the decision): Calendar events, deadlines, commitments from action-items; Other people's states/decisions from entities; Health, financial, or logistical constraints; Active scenarios that overlap. **Downstream consequences** […]. Every dependency must cite its source file: `[[personal/calendar]]`, `[[work/acme/action-items]]`, etc.

§4 Timeline Overlay:

> Map each branch's key events against the actual calendar. Cross-reference `calendar.md` for recurring routines.

§6 Write Scenario File:

> Write to `memory/cog-meta/scenarios/{slug}.md`: […]

`Activation`:

> Read scenario-calibration.md first (if it exists) for past accuracy. Then read the relevant memory files for the scenario topic.

### 4. Rewritten process steps

- **§2 Dependency Mapping** — the per-domain hot-memory / action-items / entities reads collapse into the `domain_summary` calls already made in orientation; upstream dependencies come from each domain's `hot_memory` + `recent_observations`, downstream consequences from `open_action_count` + the same observations. Overlap with active scenarios comes from the `scenario_check` `path` list. Wiki-link citations (`[[personal/calendar]]`, etc.) are unchanged — the rewrite reduces *fetches*, not the citation convention.
- **§4 Timeline Overlay** — `calendar.md` stays a direct read. The cross-reference work itself is LLM reasoning, not retrieval.
- **§6 Write Scenario File** — single `write` to `cog-meta/scenarios/{slug}.md`. Already a single call; not a rewrite target per the doc-level guardrails.
- **Assumption verification** (when `/reflect` later checks the scenario) — explicitly **no RPC**. Per RPC-CONSOLIDATION.md §10: *"Just the schedule — assumption-verification is read-and-reason work that stays with the LLM."* This stays prose: the agent reads the scenario file, walks each `**Assumptions**` line, and verifies against current memory using whatever RPCs fit those specific facts (typically `domain_summary` + `recent_observations`).

### 5. LLM-judgment-preserved callout

The daemon never decides any of the following; they remain entirely with the LLM:

- Whether an input clears the **fork / stakes / uncertainty / time-sensitivity** bar (§1) — `scenario_check` reports schedule, not worthiness.
- Which 2–3 **branches** to generate and which non-obvious path to include (§3).
- **Canary signal** selection — the earliest observable indicator per branch (§5).
- **Confidence calibration** against `scenario-calibration.md` — reading the file is mechanical, weighting confidence on a new scenario is judgment.
- **Assumption verification** at check-by time — see §4. Schedule is RPC; verification is reasoning.
- **Anti-pattern enforcement** — declining to scenario obvious decisions, already-decided items, or recurring routines.

### 6. Round-trip delta

Before: **~14 reads** in orientation on a 3-domain-topic with ~4 active scenarios (1 hot-memory + 2 personal + 1 `domains.yml` + 9 domain files + 1 listing + ~4 scenario reads + 1 calibration), plus per-branch follow-ups during dependency mapping that re-touch the same files.

After: **~5 calls** in orientation (`scenario_check` + 3× `domain_summary` + 1 direct read of `scenario-calibration.md`) plus 1 direct read of `calendar.md` during timeline overlay. Global hot-memory comes free from `session_brief`.

Range: **~14 → ~5–6 round-trips**, roughly a 3× reduction with no change to skill semantics.

---

## `evolve`

Source: [cog-prime `.claude/commands/evolve.md`](https://github.com/marciopuga/cog/blob/main/.claude/commands/evolve.md). Systems-level architecture audit: reads continuity logs, measures structural files, proposes rule changes, regenerates the scorecard, appends observations + log entries. **Never touches memory content.**

### 1. Original orientation block

From `## Orientation (run FIRST, before any file reads)` (evolve.md lines 25–43):

```bash
# What did housekeeping and reflect change recently?
git diff HEAD~1 --stat memory/

# Detailed diff of architectural files (what you care about)
git diff HEAD~1 memory/cog-meta/patterns.md memory/hot-memory.md CLAUDE.md

# What changed in the last 24h?
find memory/ -type f -name "*.md" -mtime -1 | sort

# Current prompt weight components (quick file sizes)
wc -c memory/hot-memory.md memory/cog-meta/patterns.md memory/cog-meta/briefing-bridge.md 2>/dev/null
```

Plus the `## Memory Files` block (lines 9–23) naming six files to open by hand: `evolve-log.md`, `evolve-observations.md`, `CLAUDE.md`, `.claude/commands/housekeeping.md`, `.claude/commands/reflect.md`, and the *measure* set (`hot-memory.md`, `cog-meta/patterns.md`, plus every `*/patterns.md` satellite).

Concrete shape: **4 shell scans + 6 named file reads + N satellite pattern reads** (one per active domain) before audit work begins. On a 4-domain setup, that's ~14 ops.

### 2. Rewritten orientation block

Two RPC calls cover the architectural envelope:

1. `session_brief(role)` → returns the standard session-start envelope. The field that drives evolve's downstream decisions is **`recent_observations`** — answering "what did housekeeping/reflect actually do?" without re-reading every domain file. Global `hot_memory` body comes back in the same envelope.
2. `housekeeping_scan(role)` → already collapses architectural health: per-file `lines` / `size_bytes` for hot-memory, `cog-meta/patterns.md`, and every `*/patterns.md` satellite; entity-file fan-out; cap utilization. The **`per_file`** map is the scorecard substrate and the bloat signal in one call. Per RPC-CONSOLIDATION.md §2, this is the named replacement for the orientation shell pipeline.

Plus one targeted RPC for the entity ratio:

3. `entity_audit(role)` → returns `total_entries`, `total_lines`, and violation counts; the compression ratio `total_lines / total_entries` is a divide on the response, not a multi-file scan.

Three direct `cog_read` calls remain because they're code-like rule references, not memory content, and no RPC covers them: `CLAUDE.md`, `.claude/commands/housekeeping.md`, `.claude/commands/reflect.md`. Two more direct reads for evolve's own continuity logs (`cog-meta/evolve-log.md`, `cog-meta/evolve-observations.md`) — `recent_observations` is domain-scoped and doesn't cover the cog-meta evolve streams.

### 3. Original process steps involving memory reads

§2 *Process Effectiveness Audit*:

> Review the output of recent housekeeping and reflect runs […]
> **Scorecard metrics** — measure and record in evolve-log:
> - Core `patterns.md`: line count / 70, byte size / 5.5KB (target: ≤1.0)
> - Satellite pattern files: list each with line count (soft cap: 30)
> - Entity compression ratio: `(total entity lines across all files) / (total ### entries)` (target: ≤3.0)
> - Hot-memory line counts vs caps

§5 *Generate Scorecard*:

> Overwrite `memory/cog-meta/scorecard.md` with current metrics: […] (re-derives the same numbers a second time)

§6 *Write Observations & Update Log*:

> **Observations** — Append to `memory/cog-meta/evolve-observations.md` […]
> **Evolve Log** — Append to `memory/cog-meta/evolve-log.md` […]

Each scorecard bullet is at least one file open + grep/wc-style scan; satellite enumeration multiplies by domain count; §5 then re-fetches everything to write the scorecard file.

### 4. Rewritten process steps

- **§1 Architecture Review** — pure LLM judgment over the `housekeeping_scan` envelope + the two skill rule files. No new reads.
- **§2 Process Effectiveness Audit** — "did housekeeping prune the right things?" answered by diffing the `recent_observations` tail (from `session_brief`) against `housekeeping_scan.findings`. "Are pattern files under cap?" → `housekeeping_scan.per_file` for core + every satellite in one call. "Entity compression ratio" → `entity_audit` response, divide. Zero per-file re-reads.
- **§5 Generate Scorecard** — every numeric field comes from the `housekeeping_scan` + `entity_audit` responses already in context. The `cog_write` to `cog-meta/scorecard.md` (allow-listed) is a single call. Write path — not a rewrite target per doc-level guardrails.
- **§§3, 4, 6, 7** (rule proposals, content-issue routing, observations + log appends, debrief) — write paths and judgment. `cog_append` to `evolve-observations.md` and `evolve-log.md`, `cog_write` to `scorecard.md`. Unchanged.

### 5. LLM-judgment-preserved callout

The rewrite hands the LLM **more** room for judgment, not less, by deleting mechanical scans. These stay LLM-owned:

- Reading `CLAUDE.md`, `housekeeping.md`, `reflect.md` and deciding whether the rules-as-written are firing as intended (§1).
- Distinguishing **rule drift** from one-off noise in the housekeeping/reflect output streams — when a recurring content issue should become a rule change vs. be routed back as a one-off (§2, §4).
- Tagging process-health observations correctly: `bloat`, `rule-drift`, `architecture`, `gap`, `opportunity`, `process-health` (§6).
- Writing scorecard prose framing around the numbers — numbers are RPC-fed, narrative is LLM (§5).
- The §7 debrief synthesis, including the "Next 3 evolve priorities" call.
- The whole §3 *Rule Change Proposals* loop — RPCs measure, the LLM proposes.

### 6. Round-trip delta

Before: **~10–14 memory-touching ops per run** (4 shell scans + 6 named files + N satellite reads + implicit §2/§5 re-reads). Scales with domain count.

After: **~8 ops per run** — 2 RPCs (`session_brief`, `housekeeping_scan`) + 1 RPC (`entity_audit`) + 3 direct reads for code-like rule files (`CLAUDE.md`, `housekeeping.md`, `reflect.md`) + 2 direct reads for evolve's continuity logs. Crucially, satellite enumeration stops scaling with domain count — it folds into `housekeeping_scan`.

Range: **~10–14 → ~8 round-trips**, with the satellite-scaling cliff removed.

---

## housekeeping

Source: <https://github.com/marciopuga/cog/blob/main/.claude/commands/housekeeping.md>

Housekeeping is the structural-maintenance pass: garbage-collect old
observations and completed action items into glacier, prune hot-memory
to its line cap, surface stale items and dormant domains, rebuild the
glacier and link indexes, audit entity-registry format and temporal
markers, refresh L0 headers and per-domain `INDEX.md` files, and write
the briefing-bridge that foresight will consume. It is the largest
read-only-scan command in cog-prime — every step starts with "scan all
files of kind X across all domains."

### 1. Original orientation block

> ```bash
> # What changed since last run? Focus here first.
> find memory/ -type f -name "*.md" -mtime -1 | sort
>
> # Quick entry counts for archival threshold checks (>50 = archive)
> # Add paths for any domain observations files that exist
> grep -c "^- " memory/cog-meta/self-observations.md memory/personal/observations.md memory/*/observations.md memory/*/*/observations.md 2>/dev/null
>
> # Completed action items count (>10 = archive)
> grep -c "^\- \[x\]" memory/personal/action-items.md memory/*/action-items.md memory/*/*/action-items.md 2>/dev/null
> ```
>
> Only read files that need work based on these results. Skip unchanged files.

Three shell scans across the entire memory tree, then implicit reads of
`memory/domains.yml` and per-domain `hot-memory.md` (50-line cap check)
and `cog-meta/improvements.md` (10-item cap check) before any work
begins.

### 2. Rewritten orientation block

One RPC covers the whole orient pass:

- `housekeeping_scan(role)` — returns `since`, `changed_recently[]`,
  and the `thresholds` envelope:
  `observations_over_cap[]` (with pre-bucketed `by_primary_tag` counts),
  `completed_actions_over_cap[]`, `improvements_implemented_over_cap[]`,
  `hot_memory_over_cap[]`, plus `dormant_domains[]` and
  `stale_action_items[]`. The driving fields are:
  - `changed_recently[]` → scope of every subsequent sweep (skip files
    not in this set unless the step is a full rebuild).
  - `thresholds.observations_over_cap[].by_primary_tag` → drives §1
    archival routing directly (group key = bucket name, no re-parse).
  - `thresholds.*_over_cap[]` → drives §1 archival decisions for
    action-items, improvements, and hot-memory cap enforcement.
  - `dormant_domains[]` + `stale_action_items[]` → feed §3 and §7 (the
    briefing bridge) without extra scans.

If a downstream step needs domain-level shape that `housekeeping_scan`
doesn't carry (per-domain hot-memory body for §2 pruning, full
`entities.md` block for §5b/§5c), pull it lazily with
`domain_summary(role, domain)` or `entity_audit(role)` — both already
exist.

### 3. Original process steps involving memory reads

> **§1 Garbage Collect Memory**
>
> Review and archive stale data per CLAUDE.md glacier rules. All glacier
> files must have YAML frontmatter.
> - If any `observations.md` has >50 entries, group oldest entries by
>   primary tag and move to `memory/glacier/{domain}/observations-{tag}.md`
> - If `memory/cog-meta/self-observations.md` has >50 entries, group by
>   primary tag → `memory/glacier/cog-meta/observations-{tag}.md`
> - If any `action-items.md` has >10 completed items, move to
>   `memory/glacier/{domain}/action-items-done.md`
> - Apply same logic for all domains listed in `memory/domains.yml`
> - If `memory/cog-meta/improvements.md` has >10 implemented items, move
>   to `memory/glacier/cog-meta/improvements-done-{YYYY}.md`

> **§2 Prune Hot Memory (rule-based)**
>
> Read `memory/domains.yml` to discover all active domains. Check
> `hot-memory.md` for each domain, plus the cross-domain
> `memory/hot-memory.md`. Keep ALL under 50 lines.

> **§3 Surface Opportunities & Accountability**
>
> Review all `action-items.md` files across every domain:
> - Stale items (open >2 weeks): list with age and suggested next action
> - Dormant domains: if any domain has 0 new observations in >4 weeks, flag
> - Health escalation: items open >6 months get flagged with urgency label
> - Birthday prep: if any birthday in entities.md is <2 weeks away, pull
>   interests and suggest ideas

> **§4 Rebuild Glacier Index**
>
> Scan all `memory/glacier/**/*.md` files. Extract YAML frontmatter.
> Write results to `memory/glacier/index.md`.

> **§5 Link Audit (discover missing links)**
>
> For each non-glacier memory file: scan for names matching `### <Name>`
> headers in entities.md — add `[[links]]` if missing; add cross-domain
> references; link action item references.

> **§5b Entity Registry Format Enforcement**
>
> Scan all `entities.md` files for registry format compliance: 3-line
> max, glacier candidates (`status: inactive` or `last:` >6mo), missing
> `status:` / `last:` fields.

> **§5c Temporal Fact Maintenance**
>
> Scan all `entities.md` files for `(until YYYY-MM)` markers with past
> dates; add ~~strikethrough~~ or move to `## Historical` block.

> **§6 Rebuild Link Index**
>
> Scan all memory files (excluding `glacier/`) for `[[wiki-links]]`. For
> each link, record: target → source. Rewrite `memory/link-index.md`.

> **§8 L0 Header Maintenance**
>
> Check all active memory files for missing `<!-- L0: ... -->` headers.

> **§9 Rebuild Domain Indexes**
>
> Regenerate `INDEX.md` for each domain directory. List `.md` files,
> extract L0 summaries, write the table.

### 4. Rewritten process steps

Step-by-step replacement; the file-scan loops above collapse into
dedicated RPCs. Writes (archival moves, the `index.md` / `link-index.md`
/ `briefing-bridge.md` rewrites, `cog_patch` / `cog_append` for
hot-memory trims and entity fixes) stay as direct
`cog_append` / `cog_patch` / `cog_write` calls — they're already single
calls and write paths are out of scope per guardrails.

- **§1 garbage collect** — `housekeeping_scan` from orientation already
  carries `observations_over_cap[].by_primary_tag`,
  `completed_actions_over_cap[]`, and
  `improvements_implemented_over_cap[]`. No re-scan needed. For each
  flagged file, the LLM picks which entries to move (judgment: which
  rows count as "oldest" within a tag bucket when timestamps are
  irregular) and issues the move via `cog_append` to the glacier path +
  `cog_patch` to remove from the source. The frontmatter for the new
  glacier slab is composed by the LLM and written with `cog_write`
  (allow-list covers `glacier/**/*.md`).
- **§2 prune hot-memory** — `housekeeping_scan.thresholds
  .hot_memory_over_cap[]` identifies files that exceed 50 lines without
  reading them. For each one, `domain_summary(role, domain)` returns
  the current `hot_memory` body; the LLM applies the priority order
  (resolved → past-events → SSOT-violations → stale → low-signal) and
  the trim lands as a `cog_patch`. SSOT-violation detection still needs
  the model to compare hot-memory lines against the canonical file's
  content — no RPC enforces that semantic equality.
- **§3 stale items + dormant domains + birthday prep** — the first two
  come straight from `housekeeping_scan.stale_action_items[]` and
  `dormant_domains[]` (no extra calls). Health escalation (open >6
  months) is the same `stale_action_items[]` row filtered by `age_days`
  — drive the urgency label off that field. Birthday prep is the
  exception: `entity_audit(role)` returns entity blocks with metadata
  but does not parse `birthday:` / `dob:` fields. **No RPC** covers a
  birthday-window scan; fall back to `domain_summary(role, "personal")`
  for the body and let the LLM scan for upcoming dates. (Logged under
  Gaps.)
- **§4 rebuild glacier index** — `glacier_index_compute(role)` returns
  the full `entries[]` array (`path, domain, type, tags, date_range,
  entries, summary`) in one call; the LLM renders the markdown table
  and writes via `cog_write` (glacier/index.md is on the allow-list).
- **§5 link audit** — `link_audit(role)` returns
  `candidates[{source_path, line, entity_name, target_link, context}]`.
  The LLM decides which references are substantive enough to patch in
  (the candidate set is suggestions, not auto-edits), then issues
  `cog_patch` per accepted link.
- **§5b entity format enforcement** + **§5c temporal facts** —
  `entity_audit(role)` returns `format_violations[]`,
  `glacier_candidates[]`, `missing_metadata[]`, and
  `temporal_violations[]` in one envelope. The LLM applies the fixes:
  format compression / `~~strikethrough~~` / move-to-Historical /
  glacier promotion. All edits are `cog_patch`; promotions to glacier
  use `cog_append` + `cog_patch` of the source stub.
- **§6 rebuild link index** — `link_index_compute(role)` returns the
  reverse index `[{target, sources[]}]`; render and write via
  `cog_write` to `link-index.md` (on the allow-list).
- **§7 write briefing bridge** — pure write step. The content is
  assembled from data already on hand:
  `housekeeping_scan.stale_action_items` (Stale Items section),
  `housekeeping_scan.dormant_domains` (Dormant Domains section),
  filtered stale items with `age_days > 180` (Health Escalation), and
  the §3 birthday output (Birthday Prep). Write via `cog_write` to
  `cog-meta/briefing-bridge.md` (on the allow-list).
- **§8 L0 header maintenance** — no RPC enumerates files missing the
  `<!-- L0: ... -->` header. `housekeeping_scan.changed_recently[]`
  bounds the scope; for each candidate, `cog_outline(path)` reveals
  whether the L0 comment is present without a full read. When absent,
  the LLM reads, composes the one-liner, and writes via `cog_patch`
  inserting after the `# Title` line. (Logged under Gaps.)
- **§9 rebuild domain indexes** — for each domain returned by
  `session_brief(role).domains`, call `domain_summary(role, domain)`
  once to get `files_present[]` and (lazily) the L0 summaries; render
  the per-domain `INDEX.md` table and write via `cog_write`
  (`**/INDEX.md` is on the allow-list). No L0-aggregation RPC exists,
  so the per-file L0 extraction happens from the `domain_summary`
  envelope rather than N individual reads.

### 5. LLM-judgment-preserved callout

The rewrite keeps the model on the hook for everything that is not a
mechanical fetch or a structural rewrite:

- Picking *which oldest entries* to move when archiving an
  observations file by primary tag (timestamps can be irregular;
  bucket boundaries are a judgment call).
- Composing the YAML frontmatter for newly-created glacier slabs
  (domain, type, tags, date_range, summary text).
- Pruning hot-memory by the five-rule priority order: which lines are
  resolved, past-events, SSOT-violations, stale, low-signal. The RPC
  proves only that the file is *over cap* — what to cut is judgment.
- Detecting SSOT violations between hot-memory lines and canonical
  files (no RPC enforces semantic equality of two prose lines).
- Deciding which `link_audit` candidates are "substantive enough" to
  patch — `link_audit` proposes, the LLM disposes.
- Composing each L0 one-liner (≤80 chars summarizing file purpose) and
  the per-domain INDEX summary header.
- Writing the briefing-bridge: choosing which stale items deserve a
  named suggested-action vs grouping under the "stale >4 weeks → one
  line per domain" compression rule.
- Phrasing the §10 debrief.

### 6. Round-trip delta

Original orient + scan + read passes against a representative ~6-domain
memory tree (counting each shell scan, each per-file read needed by §1,
§2, §3, §5, §5b, §5c, §6, §8, §9):

- 3 shell scans (find / grep observations / grep completed actions)
- 1 `domains.yml` read
- ~6 × `hot-memory.md` reads (§2 cap check)
- ~6 × `observations.md` reads (§1 archival routing)
- ~6 × `action-items.md` reads (§3 stale-item scan)
- ~6 × `entities.md` reads × 2 (§5 link audit + §5b/§5c format/temporal)
- N glacier reads for §4 index rebuild (~10–30 on a mature tree)
- M memory file reads for §6 link-index rebuild (excluding glacier;
  typically 25–40 on a 6-domain tree)
- ~6 × `INDEX.md` regeneration reads (§9, plus per-file L0 extraction)
- L0 sweep (§8) reads on the changed-files subset (typically 5–15)
- 1 `improvements.md` read for cap check

**Original total: ~80–110 file/scan operations per pass.**

Rewritten:

- 1 `housekeeping_scan` (covers §1 + §2 + §3 cap checks, dormant
  domains, stale action items in one envelope)
- 1 `glacier_index_compute` (covers §4)
- 1 `link_audit` (covers §5)
- 1 `entity_audit` (covers §5b + §5c)
- 1 `link_index_compute` (covers §6)
- 1 `session_brief` (orientation; supplies the domain list for §9)
- ~6 × `domain_summary` (one per in-scope domain — needed for §2
  hot-memory body, §3 birthday-scan fallback, and §9 INDEX.md
  rebuild; collapsible to "only domains touched this pass" via the
  `changed_recently[]` filter)
- ~5–15 × `cog_outline` for L0 presence checks on changed files (§8)
- a small handful of targeted `cog_read` calls for the residual
  judgment passes (SSOT violations in §2, substantive-link confirmation
  in §5, birthday-date extraction in §3) — typically 3–8

**Rewritten total: ~20–35 RPC/read operations per pass.**

Round-trip delta: **≈ 3–4× fewer fetches**, with the scan-every-file
loops in §1, §3, §4, §5, §5b, §5c, §6 each collapsed into a single
typed RPC and the per-domain INDEX rebuild bounded by `domain_summary`
rather than N raw reads.

---

## scenario

Source: <https://github.com/marciopuga/cog/blob/main/.claude/commands/scenario.md>

Decision-modeling skill: take a decision point, branch into 2–3 paths, ground each in real memory data, map dependencies + timelines + canary signals, and write to `cog-meta/scenarios/{slug}.md`.

### 1. Original orientation block

From `## Memory Files`:

> Read based on scenario topic — this is focused, not a broad scan:
> - `memory/hot-memory.md` (cross-domain strategic context)
> - `memory/personal/calendar.md` (upcoming timeline for overlay)
> - `memory/personal/action-items.md` (existing commitments, constraints)
> - Work domain action-items (read `memory/domains.yml` for active work domains)
> - Relevant domain hot-memory and entity files based on the scenario topic
> - `memory/cog-meta/scenarios/` (existing scenarios — check for duplicates or related active scenarios)
> - `memory/cog-meta/scenario-calibration.md` (past accuracy — calibrate confidence accordingly)

Concrete shape on a 3-domain-topic with ~4 active scenarios: 1 hot-memory read + 2 personal reads (calendar, action-items) + 1 `domains.yml` read + ~3D domain-file reads (hot-memory, action-items, entities per topic-relevant domain) + 1 `cog-meta/scenarios/` listing + ~4 scenario-file reads to dedupe + 1 calibration read ≈ **~14 reads** before any branching work begins.

### 2. Rewritten orientation block

Two RPC calls cover the same ground:

1. `scenario_check(role)` → `{scenarios: [{path, check_by, status, days_until_check}, …]}`. The **`path`** + **`status`** fields drive dedupe ("is this decision already an active scenario?") and contingency framing ("is this related to one due now?"). Replaces the `cog-meta/scenarios/` listing + per-file dedupe reads.
2. For each domain D the user's prompt touches, `domain_summary(role, D, since="7d")` → `{hot_memory, open_action_count, recent_observations, files_present, …}`. The **`hot_memory`** body + **`recent_observations`** array feed dependency mapping; **`open_action_count`** is the early-exit signal ("decision is already committed, don't scenario it"). Replaces the per-domain hot-memory + action-items + entities reads plus the `domains.yml` lookup.

Two reads stay as direct file fetches (no RPC, called out per the guardrails):

- `cog-meta/scenario-calibration.md` — confidence-calibration source. Single file, low frequency. **Direct read.**
- `personal/calendar.md` — week-grain calendar overlay. **Direct read.**

The global `memory/hot-memory.md` drops out: the session-start `session_brief` call (run once per conversation) already returned it.

### 3. Original process steps involving memory reads

§2 Dependency Mapping:

> Read across memory files to identify what this decision depends on and what depends on it. **Upstream dependencies** (things that constrain the decision): Calendar events, deadlines, commitments from action-items; Other people's states/decisions from entities; Health, financial, or logistical constraints; Active scenarios that overlap. **Downstream consequences** […]. Every dependency must cite its source file: `[[personal/calendar]]`, `[[work/acme/action-items]]`, etc.

§4 Timeline Overlay:

> Map each branch's key events against the actual calendar. Cross-reference `calendar.md` for recurring routines.

§6 Write Scenario File:

> Write to `memory/cog-meta/scenarios/{slug}.md`: […]

`Activation`:

> Read scenario-calibration.md first (if it exists) for past accuracy. Then read the relevant memory files for the scenario topic.

### 4. Rewritten process steps

- **§2 Dependency Mapping** — the per-domain hot-memory / action-items / entities reads collapse into the `domain_summary` calls already made in orientation; upstream dependencies come from each domain's `hot_memory` + `recent_observations`, downstream consequences from `open_action_count` + the same observations. Overlap with active scenarios comes from the `scenario_check` `path` list. Wiki-link citations (`[[personal/calendar]]`, etc.) are unchanged — the rewrite reduces *fetches*, not the citation convention.
- **§4 Timeline Overlay** — `calendar.md` stays a direct read. The cross-reference work itself is LLM reasoning, not retrieval.
- **§6 Write Scenario File** — single `cog_write` to `cog-meta/scenarios/{slug}.md`. Already a single call; not a rewrite target per the doc-level guardrails.
- **Assumption verification** (when `/reflect` later checks the scenario) — explicitly **no RPC**. Per RPC-CONSOLIDATION.md §10: *"Just the schedule — assumption-verification is read-and-reason work that stays with the LLM."* This stays prose: the agent reads the scenario file, walks each `**Assumptions**` line, and verifies against current memory using whatever RPCs fit those specific facts (typically `domain_summary` + `recent_observations`).

### 5. LLM-judgment-preserved callout

The daemon never decides any of the following; they remain entirely with the LLM:

- Whether an input clears the **fork / stakes / uncertainty / time-sensitivity** bar (§1) — `scenario_check` reports schedule, not worthiness.
- Which 2–3 **branches** to generate and which non-obvious path to include (§3).
- **Canary signal** selection — the earliest observable indicator per branch (§5).
- **Confidence calibration** against `scenario-calibration.md` — reading the file is mechanical, weighting confidence on a new scenario is judgment.
- **Assumption verification** at check-by time — see §4. Schedule is RPC; verification is reasoning.
- **Anti-pattern enforcement** — declining to scenario obvious decisions, already-decided items, or recurring routines.

### 6. Round-trip delta

Before: **~14 reads** in orientation on a 3-domain-topic with ~4 active scenarios (1 hot-memory + 2 personal + 1 `domains.yml` + 9 domain files + 1 listing + ~4 scenario reads + 1 calibration), plus per-branch follow-ups during dependency mapping that re-touch the same files.

After: **~5 calls** in orientation (`scenario_check` + 3× `domain_summary` + 1 direct read of `scenario-calibration.md`) plus 1 direct read of `calendar.md` during timeline overlay. Global hot-memory comes free from `session_brief`.

Round-trip delta: **~14 → ~5–6 round-trips, roughly a 3× reduction** with no change to skill semantics.

---

## Gaps surfaced

(Populated by the per-skill sections as they uncover needs the
current RPC vocabulary can't meet. No new RPCs are *proposed* here —
this is a needs log for a later design pass.)

- **Project-file reads outside `memory/`** — `setup` needs to read
  `CLAUDE.md` and list `~/.claude/projects/<slug>/` to discover the
  Claude Code transcripts directory. Neither is Cog memory, so the
  RPC layer correctly doesn't cover them. Flagging so a future
  "project-fs" facade isn't accidentally rolled into the memory RPC
  surface.
- **housekeeping: `birthday_scan(role, within_days)`** — §3 birthday
  prep wants "entities with `birthday:` / `dob:` falling in the next N
  days, with `interests` field returned for gift-idea synthesis."
  `entity_audit` carries metadata-violation flags but does not parse
  date fields or window them. Currently falls back to a full
  `domain_summary("personal")` plus LLM date arithmetic.
- **housekeeping: `l0_audit(role, scope?)`** — §8 wants "files missing
  the `<!-- L0: ... -->` header, scoped to changed-recently or full
  tree." No RPC enumerates this; current rewrite uses
  `cog_outline` per candidate file, which works but is N round trips
  on the changed subset. A bulk version would let the L0 sweep be a
  single call before any patching.
