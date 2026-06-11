---
name: evolve
description: Monthly architecture audit of the memory system with threshold-routed actions
triggers: [evolve, audit, scorecard, architecture review]
---

<!-- Canonical playbook — SSOT in cogmemory docs/llm/skills/. Hosts vendor this file and apply HOST-ADAPTATION blocks. Reference host implementation: ytsejam server/skills/. -->

# Evolve

Systems-level self-improvement. The architect.

**This is NOT reflect.** Reflect = "what did I learn?" Evolve = "are the rules working?" Evolve never touches memory content — it changes (or proposes changes to) the rules that govern how content moves.

## Memory Path

All paths are relative to the memory root. Role is auto-injected by the tools — never construct absolute paths.

## Minimum Data Check

Before auditing, verify the system has enough history:
- If reflect has never run (no self-observations, no patterns): stop. Say "Nothing to audit yet. Run reflect a few times first to build patterns, then evolve can assess whether they're working."
- If `cog-meta/patterns.md` has < 3 entries: say "Too few patterns to evaluate effectiveness. Let the system run for a few more cycles."

Evolve audits rules — there need to be rules to audit.

## Orientation (run FIRST, before audit work)

Four RPCs cover the architectural envelope:

1. `cog_rpc("session_brief")` — the session-start envelope: hot-memory body, patterns, and the **domain list with paths** (keep the paths, you need them for INDEX freshness below). What housekeeping/reflect actually did comes from a SEPARATE call: `cog_rpc("recent_observations", {"since": "30d"})` — it is NOT a field of session_brief.
2. `cog_rpc("housekeeping_scan")` — the structural-health envelope. Fields: `since`, `changed_recently[]`, `thresholds{observations_over_cap[], completed_actions_over_cap, improvements_implemented_over_cap, hot_memory_over_cap, patterns_over_cap[]}`, `dormant_domains[]`, `stale_action_items[]`. This is the scorecard substrate and the bloat signal in one call.
3. `cog_rpc("entity_audit")` — returns `total_entries` and `total_lines`; the entity compression ratio is `total_lines / total_entries`, a divide on the response, not a multi-file scan.
4. `cog_rpc("recent_observations", {"since": "30d"})` — what housekeeping/reflect/you actually did recently; drives the process-effectiveness audit (§2) and rule-drift detection.

Plus direct reads for evolve's own continuity (rule references, not memory content):

- `cog_read("cog-meta/self-observations.md")` — what's been noticed
- `cog_read("cog-meta/patterns.md")` — current rules
- `cog_read("cog-meta/improvements.md")` — open proposals (check before filing new ones; don't duplicate)

And load the sibling playbooks whose rules you're auditing:

- `skill("housekeeping")` — the housekeeping rules as written
- `skill("reflect")` — the reflect rules as written

Do not edit memory content. Measure it.

## Process

### 1. Architecture Review

Evaluate structural design:
- **Tier design** — are hot/warm/glacier boundaries well-defined?
- **Consolidation pipeline** — is the flow working? Where does it stall? (Read `dormant_domains[]` and `stale_action_items[]` from the scan.)
- **File organization** — any files in wrong domains? Orphaned files? (`cog_list()` if you need the full tree; `changed_recently[]` for the active edge.)
- **Skill boundaries** — are housekeeping/reflect/evolve lanes clean? Audit the playbook text loaded via `skill("housekeeping")` and `skill("reflect")` against what `recent_observations` shows they actually did.

### 2. Process Effectiveness Audit

Review output of recent housekeeping and reflect runs (the `recent_observations` envelope from orientation, diffed against the housekeeping_scan thresholds):

**Housekeeping check:**
- Did pruning priority order work?
- Are glacier thresholds (50 obs, 10 items) right? (`thresholds.observations_over_cap[]`, `thresholds.completed_actions_over_cap`, `thresholds.improvements_implemented_over_cap`)
- Is the 50-line hot-memory cap appropriate? (`thresholds.hot_memory_over_cap`)

**Reflect check:**
- Did consolidation produce useful patterns or noise?
- Did thread detection work?
- Is reflect staying in its lane?

**Scorecard metrics:**
- Core `patterns.md`: line count / 70 (target: ≤1.0) — from `thresholds.patterns_over_cap[]`
- Satellite pattern files: list each over its cap (cap: 30) — also from `thresholds.patterns_over_cap[]`
- Entity compression ratio: total entity lines / total entries (target: ≤3.0) — from `entity_audit` (`total_lines / total_entries`)
- Hot-memory line counts vs 50-line cap — from `thresholds.hot_memory_over_cap`
- Domain INDEX.md freshness: last-updated date vs today (target: <7 days) — for each domain path from session_brief, `cog_read("{domain_path}/INDEX.md", start=1, end=5)` and parse the header timestamp
- Temporal markers: count of expired-but-not-swept markers (target: 0) — `cog_search` for expiry markers and check dates by judgment

### 3. Auto-Route on Threshold Breach

This is the critical difference between theatrical evolve (reporting problems) and effective evolve (resolving them). When scorecard metrics breach thresholds, **create concrete action items** — not observations.

**Threshold → Action routing:**

| Metric | Threshold | Action |
|--------|-----------|--------|
| `patterns.md` line ratio > 1.0 | Exceeds 70 lines | → `cog-meta/action-items.md`: "Merge or replace patterns to bring below 70 lines" |
| Satellite pattern file > 30 lines | Exceeds soft cap | → domain `action-items.md`: "Compress {domain} patterns" |
| Entity compression > 3.0 | Entries too verbose | → domain `action-items.md`: "Compress entities or promote to threads" |
| Hot-memory > 50 lines | Exceeds cap | → `cog-meta/action-items.md`: "Prune {domain} hot-memory (run housekeeping)" |
| INDEX.md > 14 days stale | Drift risk | → `cog-meta/action-items.md`: "Rebuild domain indexes (run housekeeping)" |
| Expired temporal markers > 0 | Stale facts | → `cog-meta/action-items.md`: "Sweep expired temporal markers (run housekeeping)" |
| Same issue logged 3+ times in self-observations | Recurring unresolved | → Escalate: propose rule change that prevents recurrence |

Routed items land via `cog_append` into `cog-meta/action-items.md` (or the domain's `action-items.md` where the table says so).

**Format for auto-routed items:**
```
- [ ] [evolve] {description} | due:YYYY-MM-DD | pri:med | added:YYYY-MM-DD
```

The `[evolve]` tag identifies items created by this skill. If an `[evolve]` item already exists for the same metric, update it (`cog_patch`) — don't duplicate.

**Key principle:** If you're logging an observation about a problem for the third time, that's a rule failure. Stop observing and start fixing.

### 4. Rule Change Proposals

Based on findings, propose concrete rule changes:
- What problem does it solve?
- What evidence supports it?
- What's the risk?
- Rule change (apply directly) vs convention change (propose for review)?

**Apply low-risk changes directly** — but only where the rules live in memory you can write (`cog-meta/patterns.md` via `cog_patch`).

**Conventions live in the host's system prompt and you CANNOT edit them.** The same goes for the skill playbooks themselves. For any change to conventions or playbook rules: PROPOSE it to the user instead, and file the proposal in `cog-meta/improvements.md` via `cog_append` (or `cog_patch` if updating an existing proposal). State the problem, the evidence, and the exact wording change you'd make.

### 5. Route Content Issues

When you spot content problems that aren't threshold breaches, route them:

```
→ housekeeping: entities.md at 290 lines, needs glacier pass
→ reflect: hot-memory missing link for X
→ reflect: patterns.md has stale data
```

If the same issue keeps appearing → that's a rule problem. Propose a fix (Step 4), don't just route it again.

### 6. Write Observations & Scorecard

Observations — `cog_append` to `cog-meta/self-observations.md`:
- Format: `- YYYY-MM-DD [tag]: observation`
- Tags: bloat, staleness, redundancy, gap, architecture, opportunity, rule-drift
- Max 3 per run — quality over quantity

Scorecard — `cog_write` to `cog-meta/scorecard.md`, overwriting with current metrics: each Step 2 metric with current value, target, and pass/breach. Numbers come from the RPC envelopes already in context; don't re-scan.

### 7. Debrief

Concise summary:
- *Scorecard* — metrics table with current values vs targets
- *Actions created* — items routed to action-items (list each)
- *Rule changes* — applied, or proposed to the user via improvements.md
- *Process health* — did housekeeping/reflect follow their rules?
- *Architecture notes* — structural observations

Numbers over narrative. If nothing breaches thresholds, say so and stop — don't invent work.
