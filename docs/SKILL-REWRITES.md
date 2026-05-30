# SKILL-REWRITES

Translation table mapping each cog-prime skill's memory-orientation and process steps
onto the consolidated RPC vocabulary from [`docs/RPC-CONSOLIDATION.md`](./RPC-CONSOLIDATION.md).

Each section covers one cog-prime command from
[`marciopuga/cog`](https://github.com/marciopuga/cog/tree/main/.claude/commands).
The goal is to show, concretely, where the new RPCs collapse multi-file shell scans
into a single envelope call — and where prose / LLM judgment still rules.

Sections are independent and append-only. Cards add their own section without
touching others. New unmet needs go in the **Gaps surfaced** section at the bottom.

---

## foresight

Source: [`.claude/commands/foresight.md`](https://github.com/marciopuga/cog/blob/main/.claude/commands/foresight.md)
Purpose: cross-domain strategic synthesis — read broadly, write **one** nudge to
`memory/cog-meta/foresight-nudge.md`.

### 1. Original orientation block

Quoted verbatim from cog-prime — pure file enumeration:

> 1. Read `memory/domains.yml` to discover all active domains
> 2. For each domain, read `hot-memory.md` and `action-items.md` (if they exist)
> 3. Also read:
>    - `memory/hot-memory.md` (cross-domain strategic context)
>    - `memory/personal/entities.md` (upcoming birthdays, relationships)
>    - `memory/personal/calendar.md` (what's coming up)
>    - `memory/personal/health.md` (health trajectory)
>    - `memory/cog-meta/briefing-bridge.md` (housekeeping findings)
>    - Recent observations across all domains (last 7 days)
>    - Thread current-state sections — what narratives are actively unfolding?

With N active domains, that is `1 + 2N + 5 + (recent-obs scan)` reads —
roughly **2N + 6** file fetches plus an ad-hoc grep for last-7-day observations.
For a six-domain cog (personal, work, projects, health, family, cog-meta) that
is ~18 round trips before any synthesis begins.

### 2. Rewritten orientation block

Two RPC calls cover the entire scan:

- **`session_brief`** — returns the cross-domain envelope: global `hot-memory.md`,
  domain index from `domains.yml`, and patterns. Drives the first decision:
  *which domains have signal worth descending into?* The `domains[]` field plus
  `hot_memory` summaries replace steps 1 and the cross-domain bullet of step 3.
- **`recent_observations(window_days=7)`** — returns the windowed observation
  scan with `by_domain` and `by_tag` aggregates. Drives the velocity / stall
  classification in process step 2; the `by_domain` counts make
  *Dormant* and *Stalling* directly readable instead of inferred from N greps.

For per-domain depth on the domains `session_brief` flagged hot, follow with
**`domain_summary(domain=...)`** once per surfaced domain (typically 1–3, not
all N). `domain_summary` returns hot-memory + action-items + recent
observations + entities for that domain in one envelope — replacing the
per-domain pair of reads in step 2 and the personal-domain reads in step 3.

Round-trip count for a six-domain cog: **`1 (session_brief) + 1 (recent_observations) + ~2 (domain_summary on hot domains) = ~4`** calls,
down from ~18.

### 3. Original process steps (memory reads)

Quoted:

> ### 2. Velocity & Stall Detection
>
> Scan action-items across all domains. Classify each active item:
> - **Accelerating** — multiple updates in the last week, clear momentum.
> - **Cruising** — steady progress, on track.
> - **Stalling** — no movement in 2+ weeks despite not being deferred.
> - **Dormant** — domain-level silence (0 observations in 4+ weeks).

> ### 3. Timing Awareness
>
> Read calendar and entities for upcoming events in the next 2-4 weeks.

> ### 4. Pattern Projection
>
> Read patterns and recent observations. Project forward.

Each of these steps re-reads files already opened in orientation, or asks for
date-windowed slices the LLM must compute by hand.

### 4. Rewritten process steps

- **Velocity & Stall Detection** — driven entirely by `recent_observations`'s
  `by_domain` counts (4+ week zero = Dormant; check action-items inside
  `domain_summary` for stalling items). No new reads.
- **Timing Awareness** — `domain_summary(domain="personal")` already returns
  entities + the personal hot-memory; calendar lookahead for upcoming events
  remains the LLM's prose judgment on those entries. No RPC for calendar
  windowing exists — flagged in Gaps surfaced.
- **Pattern Projection** — `session_brief` returns patterns; projection itself
  is LLM work (see callout below). The scenario-candidate detection is
  judgment on top of the same envelope; no extra read.
- **Cross-Domain Convergence Scan** — LLM scans `recent_observations.by_tag`
  + the two-to-three `domain_summary` envelopes for overlaps. No new reads.
- **Write One Strategic Nudge** — single write to
  `memory/cog-meta/foresight-nudge.md`. **Write path stays prose**, per
  guardrail.

### 5. LLM-judgment preserved

The RPCs return *envelopes*, not nudges. The following stays in the model:

- Picking **which** domains to descend into after `session_brief`. Not all
  surfaced domains warrant a `domain_summary` call; the LLM prunes.
- The convergence judgment itself — recognising that a name or theme appearing
  in `recent_observations.by_tag` across two domains is meaningful (vs.
  coincidence).
- Pattern projection ("if this continues for 2 more weeks, what happens?") —
  the RPCs surface the data; the trajectory call is the model's.
- Scenario-candidate gate (fork? stakes? closing window?) — three-part
  judgment that no envelope can collapse.
- Composing the single nudge: prioritisation across what could have been a
  list, citing ≥2 sources, ensuring non-obviousness.
- The overwrite-vs-preserve decision on `foresight-nudge.md` content (rule:
  always overwrite, but the LLM still owns the nudge body).

### 6. Round-trip delta

Original: **~2N + 6** reads (≈18 for a six-domain cog).
Rewritten: **~4** RPC calls (`session_brief` + `recent_observations` + 1–3
`domain_summary`).
Range: **4–7×** reduction depending on how many domains the LLM elects to drill
into.

---

## Gaps surfaced

<!-- Append unmet needs here. No new RPCs in scope for this rewrite series. -->

- **Calendar / date-window lookahead.** `foresight` step 3 ("Timing
  Awareness") wants events in the next 2–4 weeks from
  `memory/personal/calendar.md` and entity birthdays. No RPC scopes to a
  forward time window; today this stays a raw read + prose scan. A future
  `upcoming_events(window_days=N)` RPC would close this.
