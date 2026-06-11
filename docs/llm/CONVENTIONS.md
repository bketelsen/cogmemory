# Cog Memory Conventions (canonical)

This is the single source of truth for the conventions text every cog host injects into its LLM sessions — the successor to the original cog repo's `CLAUDE.md`. Hosts render this (minus this preamble) into the system prompt, followed by the dynamic brief: hot memory, patterns, the domain table, and open-action counts from `session_brief`. See [INTEGRATION.md](INTEGRATION.md) for the injection pattern and [RPC.md](RPC.md) for envelope shapes.

Tool names below are the canonical vocabulary (`cog_read`, `cog_write`, `cog_append`, `cog_patch`, `cog_outline`, `cog_search`, `cog_list`, `cog_move`, `cog_rpc`). Hosts should expose these names so skill playbooks port verbatim.

---

You have persistent memory across sessions, served by the cog daemon through the cog_* tools. Paths are relative to the memory root (e.g. "personal/observations.md"). Write immediately — don't wait to save something worth remembering.

### Memory tiers

- **Hot** (`*/hot-memory.md`) — loaded below every conversation, <50 lines, rewrite freely
- **Warm** (domain files) — read when a domain or skill activates
- **Glacier** (`glacier/`) — read-only YAML-frontmattered archives, cataloged in `glacier/index.md`

### Retrieval protocol

Every memory file begins with `<!-- L0: summary (max 80 chars) -->`.
1. L0 scan — `cog_rpc("l0index", {domain})` to find relevant files
2. L1 — `cog_outline(path)` to scan section headers of long files
3. L2 — `cog_read(path, section?)` — read sections, not whole files, when possible

### Memory rules

1. observations.md is append-only via cog_append: `- YYYY-MM-DD [tags]: <observation>`
2. action-items.md: `- [ ] task | due:YYYY-MM-DD | pri:high/med/low | added:YYYY-MM-DD`; check off done items with cog_patch
3. entities.md: 3-line registry — `### Name (relationship)` / facts / `status: | last:YYYY-MM-DD`
4. hot-memory.md: rewrite freely, keep under 50 lines
5. SSOT: each fact lives in exactly ONE file; others reference it with `[[domain-path/filename]]` wiki-links, added at write time
6. Temporal validity: time-bounded facts carry `<!-- until:YYYY-MM-DD grace:N -->`; stable-since facts `<!-- from:YYYY-MM-DD -->`
7. ALWAYS write to a domain's *path* from the Domains table below, never its id — the daemon rejects id-as-path writes
8. cog-meta/patterns.md: edit in place, ≤70 lines of distilled, timeless rules

### File edit patterns

| File | Pattern |
|---|---|
| hot-memory.md | Rewrite freely |
| observations.md | Append only |
| action-items.md | Append new, check off done |
| entities.md | Edit in place (3-line max) |
| cog-meta/patterns.md | Edit in place (≤70 lines) |
| Thread files | Current State: rewrite / Timeline: append |
| glacier/* | Read-only |

### Threads

Read-optimized synthesis files, raised when a topic appears in 3+ observations across 2+ weeks. Spine: Current State → Timeline → Insights. One file forever.

### Consolidation (3 gates, run by /reflect)

1. Cluster: ≥3 entries, same tag, ≥7-day span, ≥3 distinct dates, specific tag
2. Coverage: skip if an existing pattern covers it; REPLACE when a new insight subsumes an old one
3. Synthesis: one actionable line + `<!-- promoted:YYYY-MM-DD theme:tag -->` audit trail

Spike: ≥5 entries in <7 days = heating topic (thread candidate, not pattern-ready).

### Glacier thresholds (run by /housekeeping)

- observations.md >50 entries → archive oldest to `glacier/{domain-path}/observations-{tag}.md`
- action-items.md >10 completed → `glacier/{domain-path}/action-items-done.md`
- glacier files need YAML frontmatter: type, domain, tags, date_range, entries, summary

### Pipeline cadence (manual — suggest to the user, never run unasked)

Weekly: /housekeeping then /reflect in the SAME session (reflect sees cleaned state). Monthly: /evolve. /foresight weekly or on demand. Anti-pattern: running every skill every day — it's theatrical; weekly + monthly is enough.
