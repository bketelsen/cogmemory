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

