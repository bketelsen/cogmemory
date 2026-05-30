# SKILL-REWRITES — translation table

Companion to [RPC-CONSOLIDATION.md](RPC-CONSOLIDATION.md). For each cog-prime skill, this doc captures a side-by-side rewrite of the orientation and process steps using the consolidated RPC vocabulary. The goal is round-trip reduction without changing what the skill means or moving judgment work out of the LLM.

Each section is independent; cards may land them out of order. Common conventions:

- **Original** quotes the live cog-prime body verbatim (paths and prose).
- **Rewritten** swaps per-file reads for the smallest RPC that returns the same field. Where no RPC exists, the prose is preserved and called out.
- **LLM-judgment-preserved** lists the reasoning the daemon explicitly will not do.
- **Round-trip delta** is a coarse before/after read count — exact numbers depend on the user's domain shape; a range is fine.

Gaps surfaced during rewrites collect at the bottom of this doc, not as new RPC proposals.

---

## scenario

Source: [cog-prime `.claude/commands/scenario.md`](https://github.com/marciopuga/cog/blob/main/.claude/commands/scenario.md). Decision-modeling skill: take a decision point, branch into 2–3 paths, ground each in real memory data, map dependencies + timelines + canaries, and write to `cog-meta/scenarios/{slug}.md`.

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

Concrete shape: 1 read for global hot-memory; 2 reads for personal calendar + action-items; 1 read of `domains.yml`; then per topic-relevant domain D, ~3 reads (hot-memory, action-items, entities) → 3D; 1 directory listing of `cog-meta/scenarios/`; N reads of existing scenario files to dedupe; 1 read of `scenario-calibration.md`. On a 3-domain-topic, dedupe of ~4 active scenarios: **~14 reads** before any branching work begins.

### 2. Rewritten orientation block

Two RPC calls cover the same ground:

1. `scenario_check(role)` → returns `{scenarios: [{path, check_by, status, days_until_check}, …]}`. The **`path`** + **`status`** fields drive dedupe ("is this decision already an active scenario?") and the contingency framing ("is this related to one due now?").
2. For each domain D the user's prompt touches, `domain_summary(role, D, since="7d")` → returns `{hot_memory, open_action_count, recent_observations, files_present, …}`. The **`hot_memory`** body and the **`recent_observations`** array are what feed dependency mapping; `open_action_count` is the early-exit signal ("decision is already committed, don't scenario it").

Two prose-preserved reads stay as direct file fetches because there is no RPC for them:

- `cog-meta/scenario-calibration.md` — confidence calibration source. No RPC. **Read directly.**
- `personal/calendar.md` — week-grain calendar overlay. No RPC. **Read directly.**

These are two single reads. The session-start `session_brief` call (run once per conversation, not per skill) already returned the cross-domain `hot_memory`, so the global hot-memory read drops out entirely here.

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

- **§2 Dependency Mapping** — the per-domain hot-memory + action-items + entities reads collapse into the `domain_summary(role, D)` calls already made in orientation. Upstream dependencies come from each domain's `hot_memory` field + `recent_observations`; downstream consequences from `open_action_count` + the same observations stream. Overlap with active scenarios comes from the `scenario_check` `path` list. Wiki-link citations (`[[personal/calendar]]` etc.) are unchanged — the rewrite reduces *fetches*, not the citation convention.
- **§4 Timeline Overlay** — `calendar.md` stays a direct read (no RPC). The cross-reference work itself is LLM reasoning, not retrieval.
- **§6 Write Scenario File** — single `write` call to `cog-meta/scenarios/{slug}.md`. Already a single call; not a rewrite target per the doc-level guardrails.
- **Assumption verification** (when reflect later checks a scenario) — explicitly **no RPC**. Per RPC-CONSOLIDATION.md §10: *"Just the schedule — assumption-verification is read-and-reason work that stays with the LLM."* This stays prose: the agent reads the scenario file, walks each `**Assumptions**` line, and verifies against current memory using whatever RPCs are appropriate for those specific facts (often `domain_summary` + `recent_observations`).

### 5. LLM-judgment-preserved callout

The daemon never decides any of the following; they remain entirely with the LLM:

- Whether an input clears the **fork / stakes / uncertainty / time-sensitivity** bar (§1) — `scenario_check` reports schedule, not worthiness.
- Which 2–3 **branches** to generate and which non-obvious path to include (§3).
- **Canary signal** selection — the earliest observable indicator per branch (§5).
- **Confidence calibration** against `scenario-calibration.md` — reading the calibration file is mechanical, weighting confidence on a new scenario is judgment.
- **Assumption verification** at check-by time — see §4 above. Schedule is RPC; verification is reasoning.
- **Anti-pattern enforcement** — declining to scenario obvious decisions, already-decided items, or recurring routines.

### 6. Round-trip delta

Before: **~14 reads** in orientation alone on a 3-domain-topic with ~4 active scenarios (1 hot-memory + 2 personal + 1 domains.yml + 9 domain files + 1 listing + ~4 scenario reads + 1 calibration). Plus per-branch follow-ups during dependency mapping that re-touch the same files.

After: **~5 calls** in orientation (`scenario_check` + 3× `domain_summary` + 1 direct read of `scenario-calibration.md`) plus 1 direct read of `calendar.md` during timeline overlay. Global hot-memory comes free from the session-start `session_brief`.

Range: **~14 → ~5–6 round-trips**, roughly a 3× reduction with no change to skill semantics.

---

## Gaps surfaced

*(none from this section — calendar.md and scenario-calibration.md staying as direct reads is correct under the no-new-RPCs guardrail; both are single files, low frequency, and don't benefit from a daemon-side composer.)*
