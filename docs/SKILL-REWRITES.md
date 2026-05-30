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
