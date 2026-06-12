# Cog-Prime Skill Rewrites — Translation Table

> **HISTORICAL.** This document recorded the original file-ops→RPC translation
> design and is no longer maintained; some envelope fields described below
> have drifted from the code. The living integration contract is
> [`docs/llm/`](./llm/): [RPC.md](./llm/RPC.md) (envelopes),
> [CONVENTIONS.md](./llm/CONVENTIONS.md) (session conventions),
> [INTEGRATION.md](./llm/INTEGRATION.md) (hosting guide), and
> [skills/](./llm/skills/) (canonical playbooks).

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

## `housekeeping`

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

  **Write to the domain `path`, never the `id`.** Both
  `session_brief().domains[]` and the `domain_summary` envelope carry
  a `path` field (e.g. id `chapterhouse` → path
  `projects/chapterhouse`); the INDEX.md target is
  `{path}/INDEX.md`, and every row in the table is
  `{path}/{file}.md`. Constructing `{id}/INDEX.md` creates a stray
  sibling folder at the memory root — the daemon now rejects such
  writes with `invalid params` naming the correct path.

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

## `history`

Source: <https://github.com/marciopuga/cog/blob/main/.claude/commands/history.md>

Deep recursive search across all memory files to reconstruct a narrative from a natural-language query.

> Note: `history` is the worst fit in the catalog for RPC consolidation. It is deliberately free-form ("piece together a narrative from multiple entries"). The current RPC vocabulary targets *structured* scans (counts, thresholds, clusters), not arbitrary substring search. The rewrite below collapses what it can; the rest stays prose, and the missing primitive (`memory_search`) is recorded under Gaps.

### 1. Original orientation block

From `## Memory Files`:

> Read on activation:
> - `memory/hot-memory.md` (for context on what's currently relevant)

One read; trivial. Drives only "what frame is the user in right now."

### 2. Rewritten orientation block

```
session_brief(role) → { hot_memory, patterns, domains, action_counts, ... }
```

One call. `hot_memory` is the field that drives query framing ("are we mid-incident? mid-sprint?"); `domains[].id` is the field that drives Pass-1 scoping when the query is obviously domain-shaped (e.g. "history of the kanban DB corruption" → restrict to `work`-ish domains). Global hot-memory is usually already in hand from the session-start brief, so on most invocations this is effectively free.

### 3. Original process steps involving memory reads

> ### Pass 1: Locate
> - Extract keywords from the user's query (names, topics, dates, phrases)
> - `Grep path="memory/" pattern="<keyword>"` for each keyword
> - Note which files matched and how many hits
> - If >10 files match, narrow by domain or add query terms
> - If 0 matches, try synonyms or related terms
> - Check `memory/glacier/index.md` for archived data matching the query
>
> ### Pass 2: Extract
> - Read the top 3-5 most relevant files (by hit density and recency)
> - Extract the specific passages that match the query
> - Track the timeline: when did the topic first come up? How did it evolve?
>
> ### Pass 3: Synthesize
> - Combine extracted passages into a coherent answer
> - Present findings chronologically with dates

Pass 1 is N keyword greps across the whole tree (N ≈ 3–8). Pass 2 is 3–5 targeted file reads. Pass 3 is pure LLM.

### 4. Rewritten process steps

**Pass 1 — Locate.** No single RPC covers "grep across all memory files." Three partial substitutes exist; pick by query shape, then fall back to free-text search for the residual:

- *Observation-shaped* (events, dated entries, tag-driven): call `recent_observations(role, since=<wide window>, by_tag?=<extracted tag>, domain?=<from session_brief>)` (the scope param is `domain`; the old `by_domain` spelling is a deprecated alias accepted until 2026-07-12). The returned `by_domain` / `by_tag` aggregates tell you where to focus; the `entries[]` array already carries `{path, line, date, tags, text}` — that is most of Pass 2's payload for free, no extra read needed.
- *Entity-shaped* (a person, an org, a project): call `entity_audit(role)` once and inspect the returned `name` set across all `entities.md` files. This maps the entity to its canonical home before any read.
- *Glacier-shaped* ("did we ever…", "back when…"): **no RPC** covers glacier search (see Gaps). Fall back to `cog_read("glacier/index.md")` + a targeted `cog_read` of the matching slab — kept as prose, not collapsed.
- *Otherwise* (true free text): fall back to `cog_search(query)`. Treat its hits the same way you'd treat the `recent_observations` `entries[]` array — but expect less enrichment (see Gaps).

**Pass 2 — Extract.** If Pass 1 used `recent_observations` or returned a sufficient `cog_search` snippet, the matching passages are already in hand; skip the per-file `cog_read` loop. If the query needed glacier or entity drill-down, do at most one targeted `cog_read(path, section?)` per hit. Use `cog_outline(path)` first when the file is large and you only need one heading.

**Pass 3 — Synthesize.** Pure LLM; unchanged.

**Writes (prose, not RPC-collapsed).** When synthesis surfaces a gap ("found references to X in observations but no entity entry"), the follow-up is a `cog_append` to `entities.md` or a `cog_patch` of `action-items.md`. Already one call each; not a round-trip win to wrap.

### 5. LLM-judgment-preserved callout

The daemon never decides any of the following; they remain entirely with the LLM:

- **Keyword extraction** from the user's natural-language query, including synonym and near-miss reformulation when the first pass returns zero hits.
- **Query-shape classification** — observation / entity / glacier / free-text — and therefore which RPC to call. No RPC infers shape.
- **Top-N hit selection** from a noisy result set: hit density vs. recency vs. domain relevance is a judgment call.
- **Narrative construction**: chronological assembly, evolution-over-time framing, "first mentioned on … last touched on …" arcs.
- **Gap flagging**: surfacing "referenced but not in memory — want me to create an entity?" prompts.

### 6. Round-trip delta

Before: **~6–13 round-trips** on a typical query (3–8 keyword greps in Pass 1 + 3–5 file reads in Pass 2) before synthesis. Heavy queries with synonym retries push this higher.

After: **~2–4 round-trips** — 1 `session_brief` (often already cached from session start, so effectively free) + 1 shape-appropriate RPC (`recent_observations` / `entity_audit` / `cog_search`) that frequently returns enough text to skip Pass 2 entirely, plus 0–2 targeted `cog_read` / `cog_outline` for glacier drill-down.

Range: **~6–13 → ~2–4 round-trips**, roughly a 3× reduction on observation- and entity-shaped queries. **The win narrows on true free-text queries** (no obvious shape): there `cog_search` replaces the per-keyword fan-out with one call, but without the domain/tag/date enrichment a dedicated `memory_search` RPC would provide, the LLM still does extra work reconstructing context from flat hits. `history` therefore benefits less from this consolidation round than the structured-scan skills (`setup`, `scenario`, `evolve`, `housekeeping`) and is the strongest case in the catalog for adding `memory_search` in a future round.

---

## `reflect`

Source: [cog-prime `.claude/commands/reflect.md`](https://github.com/marciopuga/cog/blob/main/.claude/commands/reflect.md). Self-improvement pass: ingest recent session transcripts, sweep for contradictions, consolidate observations into patterns, audit entities, surface synthesis opportunities, check scenario windows, then *act* — append self-observations, patch patterns, fix stale hot-memory. Writes go through cog-prime's normal channels; reads are where the round-trip cost lives.

### 1. Original orientation block

From `## Orientation (run FIRST, before any file reads)` (reflect.md lines 17–31):

```bash
# What changed since last run? Focus here.
find memory/ -type f -name "*.md" -mtime -1 | sort

# L0 summaries for all domains — quick routing without opening INDEX.md files
grep -rn "<!-- L0:" memory/ --include="*.md" | grep -v glacier/ | sort

# Entry counts for files approaching archival threshold
grep -c "^- " memory/cog-meta/self-observations.md memory/personal/observations.md memory/*/observations.md memory/*/*/observations.md 2>/dev/null
```

Plus the `## Memory Files` block (lines 33–46) naming four files to open on activation (`reflect-cursor.md`, `self-observations.md`, `patterns.md`, `improvements.md`) and *referencing* "all domain observations, action-items, and hot-memory files" — i.e. fan-out scaled by domain count.

Concrete shape on a 4-domain setup: **3 shell scans + 4 named cog-meta reads + 1 `domains.yml` read + ~12 domain-file reads (4 × {observations, action-items, hot-memory})** ≈ **~20 ops before the actual reflection work starts**.

### 2. Rewritten orientation block

Two RPC calls cover the orientation envelope:

1. `session_brief(role="reflect")` → returns `hot_memory`, `patterns`, the full `domains` slice, and `action_counts` per domain. That's the L0 sweep (`<!-- L0:`-style routing) plus the domain manifest plus the cog-meta `patterns.md` body, all in one envelope. The driving field is **`domains`** — it replaces the `domains.yml` + L0-grep combo with a single typed list.
2. `housekeeping_scan(role="reflect")` → `thresholds.observations_over_cap` enumerates which observation files are anywhere near archival; `changed_recently` collapses the `mtime -1` find. The driving fields are **`changed_recently`** (focus list) and **`thresholds.observations_over_cap`** (which files reflect's §3 consolidation will actually want to scan).

Three direct `cog_read` calls remain because they're reflect's own continuity files and no RPC carries them: `cog-meta/reflect-cursor.md` (session-ingestion cursor — pure state), `cog-meta/self-observations.md` (read-then-append target, plus cap-enforcement at write time), `cog-meta/improvements.md` (read-then-triage target). `cog-meta/patterns.md` arrives via `session_brief`, so it does not need a separate read.

Net: **~5–6 ops** (2 RPCs + 3 targeted reads) before reflection work begins, regardless of domain count.

### 3. Original process steps involving memory reads

§2 *Cross-Reference Memory & Consistency Sweep*:

> "Hot-memory vs canonical sources: Read each domain's `hot-memory.md`. For every factual claim, read the canonical source file and verify."
> "Cross-file fact check: Verify facts shared between files are consistent."
> "Temporal validity check: Scan all `entities.md` files for `(since YYYY-MM)` / `(until YYYY-MM)` markers."
> "Cross-domain entity check: If the same person appears in multiple `entities.md` files across domains, check for fact duplication."

§3 *Consolidation Check + Hot-Memory Relevance*:

> "Scan all `observations.md` files and `cog-meta/self-observations.md` for clusters of 3+ entries on the same theme/tag."

§3b *Entity Registry Format Enforcement*:

> "Scan all `entities.md` files for format compliance: 3-line max … status/last fields … cross-domain pointers."

§3c *Detect Thread Candidates* + §3d *Proactive Synthesis Suggestions*:

> "Scan observations for topics that appear across 3+ dates or span 2+ weeks."
> "Gather observations — Read all `memory/*/observations.md` and `memory/*/*/observations.md` files. Filter to last 7 days. Cluster by domain. Cluster by topic."

§3e *Scenario Feedback Loop*:

> "Scan `memory/cog-meta/scenarios/` for active scenario files. For each scenario where today >= `check-by` date: read the scenario and its cited dependency files."

Each bullet today is a fan-out: §2 fact-check is "N hot-memory reads × M canonical reads"; §3 / §3c / §3d each independently re-walk every `observations.md`; §3b independently re-walks every `entities.md`; §3e lists and reads scenarios. Trivially **20–40+ memory-touching ops** per reflect pass on a real install, almost all of them re-fetches of files the previous step already touched.

### 4. Rewritten process steps

- **§1 Review Recent Interactions** — Claude Code transcript ingestion. Pure project-fs work; no Cog RPC covers it (and shouldn't — transcripts live under `~/.claude/projects/`, not `memory/`). Cursor read at orientation (§2 above) supplies `last_processed`; cursor write at end is a normal write path. Unchanged.
- **§2 Consistency Sweep** —
  - "Hot-memory vs canonical" still requires reading each domain's hot-memory and the cited canonical files; the *files to verify* now come from `session_brief.domains` rather than a fresh `domains.yml` read, but the verification reads themselves stay LLM-judgment (which claim to verify, which file is canonical for it). No round-trip change on the verification reads themselves — the win is at orientation.
  - "Temporal validity check" on entities → `entity_audit(role="reflect")` returns `temporal_violations` (`(until YYYY-MM)` markers with past dates needing strikethrough) and `format_violations` directly. Replaces the per-file regex scan.
  - "Cross-domain entity duplication check" → `entity_audit` enumerates entries per path; the LLM still decides which duplicates are legitimately domain-scoped vs. genuine drift, but no extra reads are needed to *find* them.
- **§3 Consolidation** + **§3c Thread Candidates** + **§3d Synthesis Opportunities** — `cluster_check(role="reflect", min_cluster_size=3, since="7d")` returns `by_tag`, `by_keyword`, and `thread_candidates` in one envelope. All three sub-passes collapse into one call. Per RPC-CONSOLIDATION.md §8, this is exactly the consumer the RPC was designed for; reflect's §3/§3c/§3d are *the* worked example. Per-cluster pattern-distillation writes (`cog_patch` on `cog-meta/patterns.md` or domain `patterns.md`) remain unchanged.
- **§3b Entity Registry Format Enforcement** — `entity_audit(role="reflect")` returns `format_violations` (3-line overflow with `has_detail_file` flag), `missing_metadata` (`status` / `last` gaps), and `glacier_candidates` in one call. Replaces N entity-file scans. Per-violation `cog_patch` writes unchanged.
- **§3e Scenario Feedback Loop** — `scenario_check(role="reflect")` returns the schedule slice (`due_now` / `overdue` / `active`, `days_until_check`). Assumption-verification on `due_now` / `overdue` scenarios stays LLM (reads cited dependency files and reasons). RPC-CONSOLIDATION.md §10 explicitly calls this out as the right split.
- **§4 Assess Performance**, **§5 Act on Findings**, **§6 Debrief** — pure write-and-judgment paths. `cog_append` to `self-observations.md` (with the §5 cap of "max 5 per pass" enforced by the LLM), `cog_patch` on `patterns.md` and `improvements.md`, `cog_patch` on stale hot-memory entries. Unchanged.

### 5. LLM-judgment-preserved callout

The rewrite eliminates orientation and enumeration scans, *not* judgment. These stay LLM-owned:

- **Transcript ingestion (§1).** Reading `*.jsonl` session files and extracting unresolved threads, broken promises, repeated friction, missed cues — pattern recognition over conversational prose. No RPC can do this; it's the core of the skill.
- **Hot-memory vs. canonical fact verification (§2).** RPCs don't know which file is canonical for which claim, and "canonical file always wins" requires the LLM to decide *what* the contradiction is, not just *that* there is one.
- **Cluster acting (§3).** `cluster_check` surfaces what's clustering; the decision "is this a timeless rule worth distilling into `patterns.md`?" vs. "is this a temporal blip?" stays with the model. Same for §3c thread promotion — `thread_candidates` is a suggestion list, never an auto-action.
- **Entity flag triage (§3b).** Format violations are mechanical; *fixing* them — compress in place, promote to detail file, or flag for user review when health/family-sensitive — is judgment. Reflect's "do NOT auto-fix health or family-sensitive facts" rule is an LLM-side guard, not an RPC parameter.
- **Scenario assumption-check (§3e).** Whether an assumption has broken is reading prose and reasoning. RPC just tells you which scenarios are due.
- **§4 honest self-assessment + §5 hot-memory promote/demote + §6 debrief composition.** Quality, calibration, narrative — all LLM.
- **Self-observation cap enforcement (§5).** "Max 5 per reflect pass — merge lower-signal ones." Prioritization is judgment; the cap is policy.

### 6. Round-trip delta

Before: **~20–40+ memory-touching ops per run** — orientation (~20 on a 4-domain setup) + §2 verify-fan-out + §3 / §3b / §3c / §3d each independently re-walking observations and entities + §3e scenario fan-out. Scales linearly with domain count.

After: **~10–14 ops per run** — orientation (2 RPCs + 3 targeted reads) + §2 verification reads (LLM-selected, only the files actually being fact-checked) + 1 `cluster_check` (§3 + §3c + §3d combined) + 1 `entity_audit` (§3b temporal/format/duplicate combined) + 1 `scenario_check` (§3e) + the cited-dependency reads for scenarios that are actually `due_now` / `overdue`.

Range: **~20–40 → ~10–14 round-trips**, with the domain-count scaling cliff removed from orientation, §3, §3b, §3c, §3d simultaneously. The remaining variance is exactly the cited-file verification work in §2 and §3e — which is judgment, not enumeration, and correctly stays LLM-driven.

---

## `foresight`

Source: [cog-prime `.claude/commands/foresight.md`](https://github.com/marciopuga/cog/blob/main/.claude/commands/foresight.md). Forward-looking synthesis: scan broadly across every domain, detect cross-domain convergences, classify action-item velocity (accelerating / cruising / stalling / dormant), check timing windows from calendar + entities, project patterns forward, then write *one* strategic nudge to `cog-meta/foresight-nudge.md`. Foresight is read-only on every memory file except the nudge target; the round-trip cost lives entirely in the orientation/scan fan-out, which today scales linearly with domain count and observation volume.

### 1. Original orientation block

From `## Memory Files` (foresight.md):

> "Read broadly — this is a scan, not a focused lookup:
> 1. Read `memory/domains.yml` to discover all active domains
> 2. For each domain, read `hot-memory.md` and `action-items.md` (if they exist)
> 3. Also read:
>    - `memory/hot-memory.md` (cross-domain strategic context)
>    - `memory/personal/entities.md` (upcoming birthdays, relationships)
>    - `memory/personal/calendar.md` (what's coming up)
>    - `memory/personal/health.md` (health trajectory)
>    - `memory/cog-meta/briefing-bridge.md` (housekeeping findings)
>    - Recent observations across all domains (last 7 days)
>    - Thread current-state sections — what narratives are actively unfolding?"

Concrete shape on a 4-domain setup: **1 `domains.yml` read + 4 × 2 per-domain reads (hot-memory + action-items) + 1 global hot-memory + 3 personal files (entities, calendar, health) + 1 briefing-bridge + 4 `observations.md` reads filtered to 7d + N thread-file reads** ≈ **~18–22 ops before any synthesis happens**, scaling linearly with domain count and thread count.

### 2. Rewritten orientation block

Four RPC calls cover the scan envelope:

1. `session_brief(role="foresight")` → `hot_memory`, `patterns`, the full `domains` slice (with `action_counts` per domain), and the cog-meta `patterns.md` body. Replaces the `domains.yml` read, the global `memory/hot-memory.md` read, and the per-domain `hot-memory.md` + `action-items.md` fan-out in one envelope. Driving fields: **`domains`** (the manifest foresight needs to project across) and **`action_counts`** (input to §2 velocity classification before any per-domain action-items read).
2. `recent_observations(role="foresight", since="7d")` → the "recent observations across all domains" bullet collapses into one call returning `{domain, path, date, tag, snippet}` records. Driving field: the record list itself — foresight clusters these by domain/topic to feed §1 convergence detection and §2 stall detection.
3. `housekeeping_scan(role="foresight")` → carries `briefing_bridge` (the cog-meta findings foresight is told to read) plus `changed_recently` (foresight's "what's actively unfolding" proxy — files mutated since last housekeeping pass are the live narrative threads). Replaces the standalone `cog-meta/briefing-bridge.md` read and removes the need to enumerate thread files by hand.
4. `scenario_check(role="foresight")` → `due_now` / `overdue` / `active` with `days_until_check`. Feeds §3 timing awareness and §4 pattern projection's scenario-candidate detection. Foresight is the natural *upstream* consumer of `scenario_check`: reflect closes scenario windows after they trigger; foresight surfaces the ones that should open next.

Three direct `cog_read` calls remain because they're personal-domain detail files with semantics no RPC carries:

- `personal/calendar.md` — upcoming-event windows (§3 timing awareness). Calendar entries are dated free-text; no audit RPC parses them.
- `personal/entities.md` — birthday / anniversary fields for the next 2–4 weeks (§3 timing awareness). `entity_audit` flags format violations and dormancy but does not window date fields (see Gaps).
- `personal/health.md` — health trajectory narrative for pattern projection (§4). Pure prose continuity file; no RPC covers it.

`cog-meta/foresight-nudge.md` is the write target and is overwritten each run; no read needed.

Net: **~7 ops** (4 RPCs + 3 targeted personal-domain reads) before synthesis begins, regardless of domain count or thread volume.

### 3. Original process steps involving memory reads

§1 *Cross-Domain Convergence Scan*:

> "Look for topics, people, or themes appearing in 2+ domains simultaneously. These are convergence points — where effort in one area compounds into another."

§2 *Velocity & Stall Detection*:

> "Scan action-items across all domains. Classify each active item: Accelerating … Cruising … Stalling … Dormant — domain-level silence (0 observations in 4+ weeks)."

§3 *Timing Awareness*:

> "Read calendar and entities for upcoming events in the next 2-4 weeks. Look for timing windows — things that should start NOW to be ready later."

§4 *Pattern Projection*:

> "Read patterns and recent observations. Project forward: 'If this continues for 2 more weeks, what happens?' Scenario candidate detection: if a pattern projection reveals a genuine fork … flag it as a scenario candidate."

§5 *Write One Strategic Nudge*: pure write path to `cog-meta/foresight-nudge.md`. No reads.

Today each bullet drives more reads: §1 convergence needs every domain's hot-memory + observations re-walked to find cross-references; §2 needs each `action-items.md` re-read with date arithmetic; §2 dormancy needs each domain's `observations.md` mtime + entry-date inspected; §3 needs targeted personal reads; §4 needs patterns + recent observations re-fetched. Trivially **15–25 memory-touching ops** beyond orientation on a real install, almost all of them re-fetches.

### 4. Rewritten process steps

- **§1 Cross-Domain Convergence Scan** — the convergence judgment (which topics/people/themes appear in 2+ domains) stays LLM. The *inputs* are now already loaded: `session_brief.domains` carries per-domain hot-memory; `recent_observations` returns 7-day observations tagged by domain. Clustering happens in memory, no additional reads. The "compounds into another" call is judgment, not enumeration.
- **§2 Velocity & Stall Detection** — `session_brief.action_counts` plus the items in `recent_observations` per domain give the LLM what it needs to classify Accelerating / Cruising / Stalling / Dormant without re-reading per-domain `action-items.md`. The "Dormant — 0 observations in 4+ weeks" check falls out of `housekeeping_scan.changed_recently` (the *inverse* — domains absent from changed_recently for 4+ weeks) plus the domain manifest. Per-item triage (which stall is genuine vs. deferred) stays LLM.
- **§3 Timing Awareness** — the three personal-domain reads from orientation (`calendar.md`, `entities.md`, `health.md`) cover this directly. No additional RPC; the LLM reasons over the prose for 2–4 week windows. *Gap*: birthday/anniversary date-window scanning would benefit from a `birthday_scan(within_days)` RPC (already logged by `housekeeping`); foresight is the second consumer of that hypothetical RPC.
- **§4 Pattern Projection** — `session_brief.patterns` carries the patterns file; `recent_observations` carries the 7-day data. Projection ("if this continues for 2 more weeks…") is LLM. Scenario-candidate detection cross-references against `scenario_check.active` to avoid re-proposing a scenario the user already has open — RPC removes the "did I already file this?" guesswork without taking the judgment.
- **§5 Write One Strategic Nudge** — pure `cog_write` (foresight-nudge.md is on the allow-list per `cog-housekeeping.md` and `cog-memory.md`'s write guard). Overwrite-each-run semantics unchanged. Synthesis, source-citation, "non-obvious" filter, and one-nudge prioritization all stay LLM.

### 5. LLM-judgment-preserved callout

The rewrite eliminates orientation + enumeration scans, *not* judgment. These stay LLM-owned:

- **Cross-domain convergence detection (§1).** Spotting that "Kyle" appears in `work/entities.md` and `projects/observations.md` and that the *connection* matters is pattern recognition over prose. RPCs surface the records; the LLM finds the convergence.
- **Velocity classification (§2).** "Accelerating vs. cruising vs. stalling" is a calibrated judgment about meaningful momentum, not a count. Action-item updates per week is a signal, not a verdict. The "respect deferrals" anti-pattern is explicitly LLM-side.
- **Dormancy interpretation (§2).** "Domain silent for 4+ weeks" can be conscious de-prioritization or drift. The RPC says *which* domains are silent; the LLM judges *whether* silence is a nudge candidate.
- **Timing-window judgment (§3).** "Things that should start NOW to be ready later" requires reading calendar prose, knowing the user's prep cadence, and projecting against current commitments. No RPC carries this.
- **Pattern projection (§4).** "If this continues for 2 more weeks, what happens?" is forecasting — calibrated extrapolation from the patterns + observations the RPCs serve, not the RPCs' job.
- **Scenario-candidate filtering (§4).** "Fork + stakes + closing window" is the validity test; routine decisions and hypotheticals without deadlines must be rejected. `scenario_check` tells foresight what's already filed, but never proposes new scenarios on its own.
- **The nudge itself (§5).** One-nudge-not-a-list prioritization, ≥2-source citation, "non-obvious" filter, cross-domain preference, "be actionable not 'think about X'" — every one of foresight's anti-patterns and rules is enforced by the LLM at synthesis time, not by any RPC.
- **Read-only discipline.** Foresight's hard rule "NEVER edits memory files except `foresight-nudge.md`; if you spot an error, note it in the signal section and let reflect handle it" is an LLM-side guard. RPCs are read-only by contract here, but the no-write-back rule is policy.

### 6. Round-trip delta

Before: **~33–47 memory-touching ops per run** — orientation (~18–22 on a 4-domain setup) + §1 convergence re-walks + §2 per-domain action-items + dormancy mtime checks + §3 personal reads (already in orientation but typically re-fetched) + §4 patterns + observations re-fetch + §4 scenario-candidate "have I already filed this?" sweep. Scales linearly with domain count and observation volume.

After: **~9–12 ops per run** — orientation (4 RPCs + 3 targeted personal reads) + 1 `cog_write` to `foresight-nudge.md` + occasional cited-file verification when the LLM wants to double-check a specific claim before citing it. Synthesis, classification, projection, and one-nudge prioritization happen in memory over the already-loaded RPC envelopes.

Range: **~33–47 → ~9–12 round-trips**, with the domain-count scaling cliff removed from orientation and §1/§2 simultaneously. The remaining variance is cited-file verification at write time — judgment, not enumeration, and correctly LLM-driven.

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
- **housekeeping + foresight: `birthday_scan(role, within_days)`** — §3 birthday
  prep wants "entities with `birthday:` / `dob:` falling in the next N
  days, with `interests` field returned for gift-idea synthesis."
  `entity_audit` carries metadata-violation flags but does not parse
  date fields or window them. Currently falls back to a full
  `domain_summary("personal")` plus LLM date arithmetic. Foresight's
  §3 timing-awareness pass is the second consumer — same date-window
  shape, different role tag — so the RPC should accept `role` rather
  than hardcoding housekeeping semantics.
- **housekeeping: `l0_audit(role, scope?)`** — §8 wants "files missing
  the `<!-- L0: ... -->` header, scoped to changed-recently or full
  tree." No RPC enumerates this; current rewrite uses
  `cog_outline` per candidate file, which works but is N round trips
  on the changed subset. A bulk version would let the L0 sweep be a
  single call before any patching.
- **history: `memory_search(role, query, since?, by_domain?, by_tag?, limit?)`** — unified full-text search across all memory files (observations, entities, action-items, hot-memory, glacier) with hit-density + recency ranking, returning pre-computed `{path, line, date, snippet, domain, tags}` records. `cog_search` exists but returns flat hits without the domain/tag/date enrichment that `history` Pass 1 currently builds by hand from grep output. Without this, `history` is the worst-served skill in the catalog: free-text queries still require the LLM to walk hits back to file paths and infer recency/domain context one by one.
- **history: `glacier_search(role, query)`** — targeted search inside archived slabs without rehydrating them all. Currently requires reading `glacier/index.md` and guessing which slab to crack open based on slab summaries alone.
