# Design: Tiered patterns — global + per-domain (DRAFT)

**Status:** DRAFT 2026-06-11 — to hash out further. Filed because the problem GROWS as an issue until solved (every reflect run that promotes a project-specific rule into the always-injected global tier makes it worse).
**Spans:** cogmemory daemon (`session_brief`, `housekeeping_scan`) + cog-skills (`/cog` generator, reflect, housekeeping) — NOT a single-repo change.
**Blocked on:** cogmemory is PAUSED during the ytsejam supernova burst. This is a POST-BURST evolve item.

---

## Problem

`cog-meta/patterns.md` is injected into the system prompt of EVERY chat session, unconditionally, every turn. Verified mechanism (ytsejam `server/src`): `session_brief` RPC returns the full `patterns.md` body → `cog/brief.ts renderSection` emits `### Patterns\n\n${brief.patterns}` → `persona.ts composeSystemPrompt` appends it → `manager.ts wire()` runs that closure for every session. Same path as hot-memory.

So every line of `patterns.md` is paid as input tokens on every turn of every conversation. The cap (70 lines / 5.5KB, byte-cap binding) exists precisely because of this always-loaded cost.

**The drift:** during the ytsejam burst, `patterns.md` accumulated ytsejam/harness-specific rules — e.g. systemd `/usr/bin/env` ExecStart, subagent-cwd-is-harness-data-dir (a pi-agent-core fact), dev-launcher env-isolation. These are pure tax when the active work is cogmemory, truenas-mcp, or personal/kids. The always-loaded tier is absorbing domain-specific content — a violation of "keep the always-loaded tier ruthlessly small." It will only grow as more projects come off pause and each contributes its own project-isms to the global file.

## Proposal

Split patterns into two tiers, mirroring the hot-memory architecture (global hot-memory always-injected + domain hot-memory loaded on domain activation):

- **Global `cog-meta/patterns.md`** — ONLY truly cross-project rules. Still injected every turn via `session_brief`. Tighter cap (~3.5–4KB).
- **Domain `{domain-path}/patterns.md`** — project/domain-specific rules. Loaded by the generated domain skill ON ACTIVATION (a tool-call read, mid-conversation, only when the domain triggers), NOT in the system prompt at turn 1. Soft cap ~25 lines each.

### The split test (load-bearing — or rules land in the wrong tier)
**"Would this rule be true if ytsejam didn't exist?"**
- YES → global. ("do it the right way", "infrastructure over instructions", harness-not-tools meta-rule, communication style, parallel-task-safety, subagent-PASS-verification, cheap-two-condition-measurement debugging principle.)
- NO → domain. (systemd ExecStart/env-file gotchas, subagent-cwd = pi-agent-core fact, dev.sh env-isolation → `projects/ytsejam/patterns.md`.)

### Cap reframe (important — the proposed "combined ≤ 5.5K" is the WRONG invariant)
Global and domain patterns load at DIFFERENT times via DIFFERENT mechanisms (always-in-prompt vs on-trigger-skill-read). You're rarely in two project domains at once. So bound **concurrent context**, not a global sum:
- `global ≤ ~4KB` (always loaded)
- each `domain ≤ ~2KB` (soft; loaded only when active)
- Effective in-context budget = `global + active-domain` ≈ today's 5.5K when ytsejam is active, but DOWN to ~4K + tiny-personal-patterns when doing kids/personal work. truenas-mcp's patterns never compete with ytsejam's — they're never both loaded.

## Implementation surface (post-burst)
1. **cog-skills `/cog` generator** — generated domain skill gains a "read `{domain-path}/patterns.md` on activation (if present)" step. Seed an empty domain patterns.md (with L0 header) per domain.
2. **cog-skills reflect (Gate 3)** — at promotion time, apply the split test: cross-project rule → `cog-meta/patterns.md`; domain-specific → `{domain-path}/patterns.md`. Reflect already routes domain observations; extend to patterns.
3. **cogmemory `housekeeping_scan`** — `patterns_over_cap` must check domain patterns files too (currently only `cog-meta/patterns.md`). BUNDLE with the related cap-blindness found 2026-06-11: the scan only flags files literally named `observations.md`, so `self-observations.md` silently grew to 98 entries / 72KB — the cap-check should match append-only logs / patterns files by shape, not exact filename.
4. **Migration** — split the current `cog-meta/patterns.md` by the test; move ytsejam-harness rules to a new `projects/ytsejam/patterns.md`. One-time, done when this ships.
5. **(Open) `session_brief`** — does it need to also surface the active domain's patterns? Probably NOT — domain patterns are deliberately skill-loaded-on-activation, not prompt-injected. Keep session_brief global-only; the domain skill owns domain-patterns loading. Decide at build.

## Risks / open questions to hash out
- **Turn-1 gap:** domain patterns aren't present until the domain skill fires (first relevant message). Fine for advisory rules; the can't-miss rules (safety, do-it-right) MUST stay global for this reason. Confirm the split keeps all safety-critical rules global.
- **Two-domains-at-once:** cross-domain sessions (e.g. infra + ytsejam) would load both domain patterns — acceptable, still bounded, rare.
- **Generator back-compat:** existing domains have no patterns.md; generator must seed lazily and housekeeping must tolerate absence.
- Does the domain-skill-read add meaningful latency vs prompt-injection? (One extra cog_read on activation — negligible, same as domain hot-memory today.)

## Why now (filed, not built)
Captured while the reasoning is fresh and because it compounds: every burst day adds project-isms to the global tier. Build when cogmemory comes off pause (it's a daemon + skill-generator + reflect change). Bundle the housekeeping cap-blindness fix (#3) since it's the same `housekeeping_scan` threshold code.
