---
name: history
description: Deep memory search and narrative reconstruction across observations and glacier
triggers: [history, remember when, what happened, timeline]
---

<!-- Canonical playbook — SSOT in cogmemory docs/llm/skills/. Hosts vendor this file and apply HOST-ADAPTATION blocks. Reference host implementation: ytsejam server/skills/. -->

Use this skill for deep memory search and recall. Trigger if the user says "what did I say about...", "when did we discuss...", "find that conversation about...", "history of...", or asks about past information that needs multi-file search. For simple date/keyword lookups, a single `cog_search` suffices — this skill is for when you need to piece together a narrative from multiple entries.

**This skill is READ-ONLY.** It never writes to memory — no `cog_write`, `cog_append`, or `cog_patch`. If synthesis surfaces a gap worth fixing, flag it to the user; do not fix it yourself.

## Domain

Memory recall — recursive search across all memory files, cross-referencing observations, entities, and action items.

## Memory Files

On activation:
- `cog_rpc("session_brief")` — `hot_memory` gives context on what's currently relevant; `domains` gives the domain list and paths for scoping the search

Search across:
- All `observations.md` files (personal, work domains, cog-meta)
- All `entities.md` files
- All `action-items.md` files
- All `hot-memory.md` files
- `glacier/` (via `index.md` for targeted retrieval)

## Process

### Pass 1: Locate

- Extract keywords from the user's query (names, topics, dates, phrases)
- Route by query shape:
  - **Observation-shaped** ("what happened with X", time-bounded events): `cog_rpc("recent_observations", {since: "90d"})` — widen to `"365d"` if the query reaches further back. Filter the returned entries by tag/domain yourself; the entries already carry path, date, tags, and text.
  - **Entity-shaped** (a person, place, org, or project): `cog_rpc("entity_audit")` to sweep entity registries for the name, then `cog_read` the matching `entities.md` files.
  - **Archive-shaped** (old data, "did we ever...", "back then"): `cog_read("glacier/index.md")`, then targeted `cog_read` of matching slabs.
  - **Free-text** (no obvious shape): `cog_search(query)` for each keyword.
- Note which files matched and how many hits
- If >10 files match, narrow by domain (from `session_brief`) or add query terms
- If 0 matches, try synonyms or related terms with `cog_search`
- Check `glacier/index.md` for archived data matching the query

### Pass 2: Extract

- If Pass 1 returned the matching passages directly (e.g. `recent_observations` entries or rich `cog_search` snippets), skip the file reads — the payload is already in hand
- Otherwise, read the top 3-5 most relevant files (by hit density and recency): run `cog_outline(path)` first on long files, then a sectioned `cog_read(path, section)` for just the headings you need
- Extract the specific passages that match the query
- Track the timeline: when did the topic first come up? How did it evolve?

### Pass 3: Synthesize

- Combine extracted passages into a coherent answer
- Present findings chronologically with dates
- If something seems incomplete, flag it:
  > "Found references to X in observations but no entity entry — want me to create one?"

  (Flag only — creating the entry happens outside this skill, after the user confirms.)

## Artifact Formats

**Search result**: `YYYY-MM-DD: <summary of what was found>`
**Memory gap**: `Gap: referenced but not in memory — <topic>`
**Timeline**: Chronological list of when a topic appeared and how it evolved

## Activation

Extract search terms from the user's query and begin Pass 1. Be thorough but concise in the synthesis — don't dump raw content. Remember: read-only — never write to memory from this skill.
