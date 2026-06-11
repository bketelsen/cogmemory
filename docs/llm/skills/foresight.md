---
name: foresight
description: Cross-domain strategic scan producing one forward-looking nudge
triggers: [foresight, strategic, nudge, what's coming]
---

<!-- Canonical playbook — SSOT in cogmemory docs/llm/skills/. Hosts vendor this file and apply HOST-ADAPTATION blocks. Reference host implementation: ytsejam server/skills/. -->

# Foresight

Strategic foresight — connecting dots across domains. Future-facing.

**This is NOT reflect** (past-facing) or **evolve** (system architecture). Foresight scans broadly and projects trajectories.

This skill is **read-only**: it never edits memory files. Its single write is the nudge file at the end.

## Minimum Data Check

Before scanning, verify there's enough material for meaningful foresight:
- If total observations across all domains < 10 (check `cog_rpc("recent_observations", {since: "90d"})` and `cog_rpc("stats")`): stop. Say "Not enough history for strategic foresight. Keep capturing for a few more weeks."
- If only 1 domain has data (from `session_brief` domains): say "Foresight works best with 2+ active domains (it connects dots between them). Consider adding more domains or building out existing ones."
- If no action items exist (`cog_rpc("open_actions")` is empty): say "No active items to assess velocity on. Capture some tasks first."

Don't force a nudge from thin data. One honest "not yet" is better than a weak insight.

## Orientation

Read broadly — this is a scan. Four RPC envelopes plus three targeted reads cover it:

1. `cog_rpc("session_brief")` — hot memory, patterns, and all active domains with their paths. This replaces the domain-manifest read and the per-domain hot-memory/action-items fan-out.
2. `cog_rpc("recent_observations", {since: "7d"})` — recent observations across all domains.
3. `cog_rpc("housekeeping_scan")` — dormancy signals. Fields: `since`, `changed_recently[]`, `thresholds{...}`, `dormant_domains[]`, `stale_action_items[]`. Domains in `dormant_domains[]`, or absent from `changed_recently[]` for weeks, are your silence signals.
4. `cog_rpc("open_actions")` — every active item, for velocity classification.

Then targeted reads (use `cog_outline` first on long files, then `cog_read` the sections you need):
- `cog_read("personal/entities.md")` — birthdays, relationships
- `cog_read("personal/calendar.md")` — upcoming events
- `cog_read("personal/health.md")` — health trajectory

## Process

### 1. Cross-Domain Convergence

Look for topics, people, or themes appearing in 2+ domains simultaneously — cluster the `session_brief` domain slices and `recent_observations` records by topic. These are convergence points — where effort in one area compounds into another.

### 2. Velocity & Stall Detection

Classify active items from `open_actions`, using `recent_observations` for movement:
- **Accelerating** — multiple updates last week. Signal: ride the wave.
- **Cruising** — steady progress. Signal: nothing to flag.
- **Stalling** — no movement 2+ weeks. Signal: blocked or lost priority?
- **Dormant** — domain silence 4+ weeks (in `housekeeping_scan.dormant_domains[]`, or missing from `changed_recently[]`). Signal: conscious or drift?

The RPC tells you which domains are silent; you judge whether the silence is a nudge candidate.

### 3. Timing Awareness

Read calendar and entities for events in next 2-4 weeks. Things that should start NOW to be ready later.

### 4. Pattern Projection

Read patterns (from `session_brief`) and recent observations. Project: "If this continues 2 more weeks, what happens?"

If a projection reveals a genuine decision fork — real stakes, a closing window — don't file it anywhere; flag the fork inside the nudge text itself so the user sees the choice.

### 5. Write One Strategic Nudge

Synthesize into **one nudge**. Not a list. One thing.

The nudge must:
- Cite at least 2 source files
- Be something the user hasn't explicitly asked about
- Be actionable — "do Y because of X and Z" (not "think about X")
- Connect dots across domains

Write with `cog_write("cog-meta/foresight-nudge.md", ...)`:

```markdown
# Foresight Nudge
<!-- Last updated: YYYY-MM-DD -->

## Signal
<What you noticed — raw observation from 2+ domains>

## Insight
<Why it matters — the connection or trajectory>

## Suggested Action
<One concrete thing to do>

---
Sources: [[file1]], [[file2]]
```

Overwrite each run. One nudge per run.

## Rules

1. **Read-only** — foresight NEVER edits memory files. The only write is `cog_write("cog-meta/foresight-nudge.md", ...)`. If you spot an error in memory, note it in the Signal section — don't fix it here.
2. **One nudge** — force prioritization.
3. **Evidence-based** — cite 2+ source files.
4. **Non-obvious** — should surprise. If the user already knows, pick something else.
5. **Forward-looking** — project into next week/month.
6. **Cross-domain preferred** — connections between domains are highest value.

## Anti-Patterns

- Don't repeat what housekeeping already flagged (`stale_action_items[]`, birthdays)
- Don't recommend "reflect on X" — be specific about what to DO
- Don't flag explicitly deferred items
- Don't flag things that are cruising
- Don't write a mini-briefing — one insight, one action
