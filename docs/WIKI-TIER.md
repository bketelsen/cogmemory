# Wiki Tier: A Sanctioned Narrative Layer for cogmemory

Design doc for adding a first-class **wiki tier** to the cogmemory store: a
durable, frontmatter-indexed, git-backed home for long-form synthesis — research
evaluations, tool/project knowledge, person provenance, design ideas — that the
terse hot/warm/glacier tiers are structurally unable to hold.

Status: **proposed**. Author: Miles (Mentat synthesis). Date: 2026-06-03.

---

## Premise

cogmemory's existing tiers optimize for *terse, fast-retrieval state*:

| Tier | Unit | Optimized for | Size pressure |
|------|------|---------------|---------------|
| hot | fact line / 3-line registry block | always-loaded orientation | hard cap (~50 lines) |
| warm | observation line, action item, entity block | on-demand domain working set | soft caps |
| glacier | archived slab + frontmatter | cold audit trail | unbounded, index-gated |

None of them is a home for **curated prose with provenance**: a 7KB evaluation of
a tool with a verdict, a 13-pattern synthesis, a person's clinical/biographical
page. Today that content has nowhere to live, so it either:

1. leaks into `observations.md` (append-only, un-editable — the wrong shape for
   knowledge that gets *revised*), or
2. is lost to session transcripts entirely (the deeper-retrieval weakness Brian
   named on 2026-05-22: *"good at keeping context crisp, weak at deeper
   retrieval"*).

We already have ~75 pages of exactly this content stranded in the **retired,
un-audited, DB-backed Chapterhouse wiki** at `~/.chapterhouse/wiki/`. The live
cogmemory store already contains **6 `wiki:` links pointing into that frozen
tree** — dangling pointers no tool can resolve (`link_audit`,
`link_index_compute`, `cog_search` all stop at the `wiki:` boundary). The
half-state is the worst case: a sanctioned-looking dependency into an
unsanctioned graveyard.

**This doc proposes resolving that half-state by adopting the wiki as a real,
audited tier — built deliberately smaller than the Chapterhouse original,
because cogmemory's flat-file/git substrate already solves the hardest problems
that wiki's design had to work around.**

---

## What the substrate already gives us for free

The Chapterhouse wiki redesign (PR #469 / v0.15.0) identified 15 gaps. The
majority were artifacts of having a **SQLite index that could disagree with the
files**. cogmemory has no such failure surface. Mapping their hard-won design
targets onto what cogmemory already does:

| Wiki-redesign target | cogmemory equivalent (already exists) |
|---|---|
| Single-sourced category list (GAP-01/09) | `domains.yml` — canonical, data-driven |
| Type as first-class validated field (GAP-03) | glacier slab `type:` frontmatter, parsed by `glacier_index_compute` |
| One canonical timestamp; derived index never written back (GAP-06) | `updated`/`last:` convention; indexes are computed, never stored |
| "Filesystem is truth, index is derived/rebuildable" (the whole DB caveat) | **This is cogmemory's entire design.** Their caveat is our default. |
| Reliable link model (GAP-12) | `[[wiki-links]]` + `link_index_compute` / `link_audit` |
| Discovery tool: "what's valid?" (GAP-11) | `domains_list`, `cog_outline`, `cog_l0index` |
| Stub-on-failure, never drop a source (GAP-07) | append-only observation discipline |

**Consequence:** the four hardest things in the Chapterhouse plan — the GAP-06
timestamp duplication, the filesystem-vs-DB truth problem, schema migrations, and
FTS5 — either **evaporate** (the first three) or become **a single explicit,
deferrable decision** (the fourth: retrieval). We are not porting an
architecture. We are extending existing cogmemory discipline to a new tier and
salvaging the *taxonomy and schema thinking*.

---

## Design

### Tier location

A `wiki/` directory under the memory root, a sibling of `glacier/`:

```
memory/
  hot-memory.md
  <domain>/...
  glacier/
    index.md
    <domain>/...
  wiki/
    index.md            # generated L0 catalog (like glacier/index.md)
    <category>/<slug>/index.md
    <category>/<slug>/<facet>.md
```

Pages are addressed by the existing link convention, with the path under
`wiki/`: `[[wiki/people/liam]]`, `[[wiki/research/honcho#Synthesis]]`. (Note: the
legacy `wiki:pages/...` prefix is replaced by the standard `[[wiki/...]]` form so
the existing link tooling resolves it with no special-casing — see Migration.)

### Taxonomy (the page-vs-facet law)

Two kinds of category, lifted from the Chapterhouse redesign because the design
is sound and maps ~1:1 onto cogmemory's existing memory domains:

- **Entity categories** (directory + page tree): `people`, `projects`, `tools`,
  `topics`, `research`, `ideas`, `orgs`.
- **Flat categories** (single curated file, no tree): `facts`, `preferences`,
  `routines`. *(Defer creating these until there is content; do not seed empty.)*

**The law (GAP-02, restated as cogmemory SSOT):** a concept is *either* a
first-class category *or* a **facet (sub-page / section) of an existing page** —
never a floating in-between. Example: `decisions` is not a category; it is
`wiki/projects/<slug>/decisions.md`. This is the SSOT principle applied to the
narrative tier: every fact has exactly one canonical home, and "what kind of
thing is this page" is a validated, enumerated answer — not a guess.

The canonical category list lives in **one place** and is shared by reader,
writer, and validator. Reuse the `domains.yml` pattern — either extend it with a
`wiki_categories:` block or add a parallel `wiki.yml`. (Recommendation:
`wiki.yml`, to keep domain routing and wiki taxonomy as separate concerns.)

### Frontmatter schema

Required: `title`, `updated` (YYYY-MM-DD, the **only** date field — GAP-06
collapses here), `entity_type` (validated against the category list),
`status` (validated against the lifecycle vocabulary).

Standard: `summary` (≤200 chars), `tags` (open vocabulary, auto-extends — see
below), `related` (array of `wiki/...` paths — the reliable link mechanism).

Optional: `confidence`, `contested`, `contradictions`, `autostub`, `version`.

```yaml
---
title: Honcho
summary: Eval as memory layer for Hermes agent; verdict deferred pending self-host test.
updated: 2026-05-19
entity_type: research
status: active
tags: [memory, self-hosting, agents]
related: [wiki/topics/semantic-memory-search, wiki/tools/monet]
confidence: medium
version: 1
---
```

Note `contested`/`contradictions` as **structured** fields: this lets a future
`wiki_audit` (modeled on `entity_audit`) surface contested knowledge
automatically, instead of conflict-marking living only in prose.

### Lifecycle vocabulary

`status` in `{ draft, active, archived, superseded }`.

Deliberately **one** metaphor. The Chapterhouse original carried two parallel
vocabularies (`draft->active->archived` *and* `seed->germinating->mature`) — that
is itself a GAP-02 half-state. We pick the doc-style progression and drop the
germination metaphor. Replacement is `superseded` + a `related:` link to the
successor page.

### Tag vocabulary (open, auto-extending — GAP-05)

Unknown tag -> **warning + auto-append** to a tag registry
(`wiki/_meta/taxonomy.md`), never a hard write failure. A closed tag allowlist
that blocks writes is the single most common way an agent-wiki rots: the agent
hits a wall on normal use and stops writing. The registry is descriptive, not
prescriptive.

### Retrieval

Three mechanisms ship in v1; the fourth (semantic) is explicitly deferred.

1. **L0 catalog** — `wiki_index_compute` (see RPC below) renders `wiki/index.md`,
   a table of every page's `title / category / status / tags / summary / updated`.
   This is the L0 "is this page relevant?" routing layer, identical in spirit to
   `glacier/index.md`.
2. **Tags** — topical grouping via the frontmatter `tags` field.
3. **Link graph** — `related:` arrays + body `[[wiki/...]]` links, resolved by
   extending the existing `link_index_compute` / `link_audit` to span the `wiki/`
   tree.

   **Implementation note (corrected 2026-06-03, post-smoke):** the existing
   `store/link.go` extracts links *only* from body `[[...]]` text — it does **not**
   read the `related:` frontmatter array. Since `related:` is the canonical curated
   cross-reference mechanism (Brian's decision), `link.go` must be extended to parse
   `related:` arrays as link sources. This is real Phase 1c work, not a no-op as an
   earlier draft assumed. `cog_search` and the tree-walk *do* already span `wiki/`
   with no change (verified).
4. **Semantic search — DEFERRED to Phase 2.** See "Deferred" below.

---

## RPC surface

One new RPC for v1, plus extensions to existing ones. The new RPC is a near-clone
of the glacier indexer — `store/glacier.go` + `rpc/glacier_test.go` are the
reference implementation to copy.

### `wiki_index_compute` (new)

Parse every `wiki/**/index.md` and facet file's frontmatter into a catalog.
Same shape as `glacier_index_compute`.

```json
{
  "count": 75,
  "entries": [
    {
      "path": "wiki/research/honcho/index.md",
      "category": "research",
      "title": "Honcho",
      "status": "active",
      "tags": ["memory", "self-hosting", "agents"],
      "summary": "Eval as memory layer for Hermes agent...",
      "updated": "2026-05-19",
      "related": ["wiki/topics/semantic-memory-search"]
    }
  ]
}
```

Rendered to `wiki/index.md` by housekeeping (add `wiki/index.md` to the
generated-file allow-list, alongside `glacier/index.md`).

### Extensions to existing RPCs

- **`cog_search`** — include `wiki/` in the scanned tree (it currently does not
  cross the `wiki:` boundary). Trivial path-glob extension.
- **`link_index_compute` / `link_audit`** — resolve `[[wiki/...]]` targets so the
  6 ghost links (and all future ones) become first-class, auditable links.
- **`housekeeping_scan`** — optionally flag wiki pages over a body-size cap or
  with `status: draft` older than N days (stale-draft surfacing). Low priority.

### Deferred RPCs (Phase 2+)

- **`wiki_search` (semantic)** — embedding index over page bodies. This is the
  one genuine capability gap vs. the Chapterhouse FTS5 search, and it is the one
  the corpus itself flags twice (`wiki/topics/semantic-memory-search`,
  `wiki/research/wiki-redesign`). **Do not gate the tier on it.** The tier is
  useful with L0 + tags + link graph alone. Take this on only if regex retrieval
  over the imported corpus proves insufficient in practice.
- **`wiki_audit`** — `entity_audit`-style sweep: format violations, missing
  required frontmatter, contested-without-resolution, broken `related:` links,
  stale drafts. Natural once the tier has lived a few weeks.

---

## Migration: import-and-discard

The Chapterhouse corpus is **not** a lift-and-shift. Curate on import.

### Import (curated — ~75 pages)

Bring in only the curated entity categories:
`topics/`, `tools/`, `projects/`, `research/`, `ideas/`, `people/`.

On import, normalize:
- `last_updated` (ISO timestamp) -> drop; keep only `updated` (YYYY-MM-DD).
  GAP-06 collapses; there is no derived timestamp in a flat store.
- legacy `wiki:pages/<x>` link form -> `[[wiki/<x>]]`.
- the `seed/germinating/mature` status values -> map onto
  `draft/active/archived` (seed->draft, germinating->active, mature->active).
- the 4 frontmatter-less docs (`_meta/log.md`, `_meta/taxonomy.md`,
  `agent-memory-guide.md`, `handoff.md`) -> either add frontmatter or route to
  glacier; do not import frontmatter-less into the curated tier.
- fix the stale root `index.md` and aspirational `pages/index.md` (they name
  categories/pages that do not exist on disk) — discard both; `wiki/index.md` is
  regenerated by `wiki_index_compute`.

### Discard / glacier (do NOT import)

- **`conversations/` (~180KB, 12 dated chat dumps)** — session-transcript sludge,
  not curated knowledge. Glacier it (audit trail) or discard. **Not** wiki tier.
- **`_meta/log.md` (34KB action log)** — historical; glacier if kept at all.
- the moved-page redirect stubs and one-line auto-ingest source stubs — discard;
  they are routing artifacts of the old system.

### Fix the 6 ghost links

After import, re-point the 6 live `wiki:` references onto the new tier:
- `personal/entities.md#Liam` -> `[[wiki/people/liam]]`
- `work/entities.md#Kyle Gospodnetich` -> `[[wiki/people/kyle-gospodnetich]]`
- 4 historical mentions in `projects/chapterhouse/observations.md` — these are
  append-only frozen lines; leave as-is (they are audit history, not live links),
  or fix only if `link_audit` noise warrants it.

This step alone removes the dangling-pointer defect and can be done **the moment
the tier exists**, independent of everything else.

---

## Acceptance checklist (the 15 gaps as anti-rot tests)

The Chapterhouse gap list is reusable as an acceptance checklist — each gap is "a
way a wiki tier silently rots." Most are prevented for free by cogmemory's
existing tooling; this table is the verification matrix.

| Gap | Anti-rot property | How v1 satisfies it |
|-----|-------------------|---------------------|
| 01 | reader/writer agree on categories | single `wiki.yml`, shared |
| 02 | no page-vs-facet half-states | the facet law, enforced on write |
| 03 | `entity_type` validated, no silent normalize | required, checked against `wiki.yml` |
| 04 | extraction routes through canonical resolver | n/a in v1 (no auto-ingest); manual writes only |
| 05 | tag vocabulary open, never blocks writes | auto-extend to `_meta/taxonomy.md` |
| 06 | one canonical timestamp | `updated` only; collapses on flat store |
| 07 | never silently drop a source | n/a in v1 (no auto-ingest) |
| 08 | first-class validated `status` | required, enum-checked |
| 09 | navigation data-driven | `wiki_index_compute` from frontmatter |
| 10 | all writes validated | cogmemory write path + frontmatter check |
| 11 | discovery: "what's valid?" | `wiki.yml` readable; `domains_list`-style |
| 12 | one reliable link mechanism | `related:` + `[[wiki/...]]`, audited |
| 13 | markdown renders as markdown | n/a (no UI; consumers are agents) |
| 14 | no write-only provenance | `related:`/`tags` queryable via index |
| 15 | uniform path conventions | `<category>/<slug>/index.md` enforced |

Gaps 04 and 07 are **N/A for v1** because v1 has **no auto-ingestion pipeline** —
all wiki writes are deliberate agent/human acts through the normal cogmemory
write path. Auto-ingest (and with it gaps 04/07) is a possible Phase 3, not part
of this build.

---

## Phasing

| Phase | Goal | Rough cost |
|-------|------|-----------|
| **1a** | `wiki/` tier + `wiki.yml` taxonomy + frontmatter schema + facet law | small |
| **1b** | `wiki_index_compute` RPC (clone of `glacier_index_compute`) + render `wiki/index.md` | ~1 day |
| **1c** | teach `link.go` to index `related:` frontmatter arrays (it currently reads only body `[[...]]`); verify `cog_search` spans `wiki/` (already does) | ~half day |
| **1d** | curated import (~75 pages) + normalization + fix 6 ghost links | ~half day |
| **2** | `wiki_search` (semantic embeddings) — **only if** regex retrieval proves insufficient | real cost; deferred |
| **2** | `wiki_audit` (entity_audit-style sweep) | small; deferred |
| **3** | auto-ingest pipeline (re-activates gaps 04/07 as requirements) | not scoped here |

Phase 1 is the whole decision. Phases 2 and 3 are independent, evidence-gated
follow-ons.

---

## What does NOT port (explicitly)

For the record, the DB-tied pieces of the Chapterhouse design that have **no
place** in cogmemory:

- the SQLite `wiki_pages` / `wiki_links` / `wiki_sources` tables — replaced by
  frontmatter + on-demand index compute;
- FTS5 full-text search — replaced (eventually) by an embedding index, or for v1
  by `cog_search` regex + L0 routing;
- schema migrations (`applyMigrations`, migration-11-adds-status) — replaced by
  frontmatter convention + an optional one-shot backfill on import;
- the derived `last_updated` column — collapses to `updated` only;
- `compiled_truth_hash`, `wiki_reindex` force-rebuild — the cogmemory analog is
  re-scanning frontmatter, which is cheap and git-diffable.

---

## Open questions for Brian

1. **`wiki.yml` vs. extending `domains.yml`** — recommendation is a separate
   `wiki.yml`. Confirm.
2. **Where does the imported corpus's `conversations/` go** — glacier (audit
   trail) or discard outright? Recommendation: glacier the lot under
   `glacier/chapterhouse/wiki-conversations/` with a single frontmatter slab, so
   it is searchable-if-needed but out of the curated tier.
3. **Is `people/` content (Liam, Kyle) wiki-tier or detail-file?** The wiki tier
   makes sense if there will be *more* than two people pages with real narrative.
   If it stays at two, detail files (`personal/liam.md` linked with `->`) may be
   lighter. Recommendation: wiki tier, since `people/` is a natural category and
   the family/provenance content will grow.
4. **Semantic search appetite** — is Phase 2 embeddings something you want on the
   roadmap now (informs whether v1 frontmatter should pre-reserve an embedding
   field), or genuinely deferred until proven necessary?
