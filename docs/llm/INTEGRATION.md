# Hosting Cog: Integration Guide

How to give an LLM host app (chat assistant, agent harness, IDE plugin) persistent memory backed by the cogmemory daemon. The design principle is non-negotiable: **the LLM does all the thinking; the daemon is only the storage substrate.** Hosts wire tools and inject prompts; they never interpret, route, or summarize memory themselves.

Reference implementation: ytsejam (`server/src/cog/`, `server/src/tools/cog.ts`, `server/src/skills.ts`). The defects called out below were all found and fixed there — don't relearn them.

## 1. Wire protocol

- Transport: **newline-delimited JSON-RPC 2.0 over a unix socket**. One request per line, one response line per request.
- Connections are persistent but the daemon processes a connection's lines **sequentially**. If your host executes tool calls in parallel (most do), use **one short-lived connection per request** — natural concurrency, no id-correlation state, and daemon restarts only fail in-flight calls.
- **64KB request line limit** (daemon-side scanner). An oversized request closes the connection with *no response*. Pre-reject requests over ~60KB client-side with guidance to split content into multiple `cog_append` calls, and map close-before-response to a clear "daemon restarted or request too large" error.
- **Decode responses with a streaming UTF-8 decoder** (e.g. Node `socket.setEncoding("utf8")`). Per-chunk `toString()` corrupts multibyte sequences split across chunk boundaries — silently, since JSON.parse still succeeds.
- Error codes: `-32700` parse, `-32600` invalid request, `-32601` method not found, `-32602` invalid params, `-32000` RBAC denied, `-32001` store error. Full envelope reference: [RPC.md](RPC.md).

## 2. Role injection (RBAC)

Every method takes `"role"` inside `params`. The host injects it from its own config; **the model must never control it**. Watch the spread order — `{role, ...modelParams}` lets a model-supplied `role` key win; it must be `{...modelParams, role}`. Surface RBAC denials (`-32000`) to the model verbatim; the messages are self-descriptive.

## 3. Tool surface

Expose the canonical vocabulary so skill playbooks port verbatim:

| Tool | RPC | Params |
|---|---|---|
| `cog_read` | read | `path`, `section?`, `start?`, `end?` |
| `cog_write` | write | `path`, `content` |
| `cog_append` | append | `path`, `text`, `section?` |
| `cog_patch` | patch | `path`, `old_text`, `new_text` |
| `cog_outline` | outline | `path` |
| `cog_search` | search | `query` |
| `cog_list` | list | — |
| `cog_move` | move | `from`, `to` |
| `cog_rpc` | passthrough | `method` (enum), `params?` |

`cog_rpc`'s method enum: `session_brief, domain_summary, housekeeping_scan, open_actions, recent_observations, glacier_index_compute, wiki_index_compute, link_index_compute, link_audit, entity_audit, cluster_check, scenario_check, domains.list, domains.get, l0index, stats, git, health`. Exclude the file ops from the enum — they have dedicated tools.

Put this rule in every write-tool description: *"path is the domain's directory **path** (e.g. projects/foo/notes.md), never the domain id — the daemon rejects id-as-path writes."* The daemon enforces it (`-32602` with the corrective path), but the description prevents the round trip.

Validation behaviors the model will hit (let the errors flow through; the model self-corrects):
- `append` to any `*observations.md` validates each line against `- YYYY-MM-DD [tags]: text`
- `append` with a `section` errors if the heading doesn't exist (create the heading first)
- id-as-path writes are rejected with the configured path named in the message

## 4. Session-start injection

Call `session_brief(role)` once per session start and render a memory section into the system prompt:

1. The full [CONVENTIONS.md](CONVENTIONS.md) text (static, ship it with the host)
2. `hot_memory` and `patterns` verbatim
3. A domain table: **id, path, label, triggers** — the path column is what makes id-as-path mistakes impossible
4. Open-action counts (and a high-priority flag from `_pri_high_anywhere`)
5. A warning line when `controller_last_error` is non-null

Operational requirements, learned the hard way:
- **Never block session start on the daemon.** Cap the fetch (~1.5s) and cache the rendered section (~60s TTL).
- **Cache failures briefly, not for the success TTL** (~5s) — one transient miss must not leave sessions memory-less for a minute.
- On failure with a previous good brief: serve it with a staleness note. With none: render an explicit "memory daemon unreachable — cog_* tools will fail, everything else works" section.
- Dedupe concurrent cold-cache fetches (sessions often start in bursts).

## 5. Skills hosting

Skills are markdown playbooks the LLM loads and follows — the host executes nothing. Canonical copies live in [skills/](skills/); each marks its **HOST-ADAPTATION** points (e.g. reflect's transcript mining).

- Store skills in a host data directory; ship the canonical six as seeds, **copy-if-missing** so user edits and generated skills survive upgrades.
- Expose a `skill(name)` tool returning the playbook text.
- Render a routing table into the system prompt from skill frontmatter (`name`, `description`, `triggers`): name | purpose | invoke-when. Escape `|` in cell text.
- The `/cog` setup playbook **generates a per-domain skill** into the same directory at the end of domain creation (frontmatter triggers from the domain manifest), which self-registers in the routing table on the next session.

## 6. Subagents / workers

Background workers should get **no cog tools and no memory prompt sections** by default. Worker output flows back through the parent session, which owns all memory writes. Grant read-only access deliberately if a use case demands it, with a restricted role.

## 7. Things the daemon will NOT do for you

By design (thinking stays in the LLM):
- No L0 header creation or repair — `l0index` *omits* headerless files; absence from it is the missing-header signal. The housekeeping playbook repairs globally.
- No automatic memory injection beyond what you render from `session_brief`.
- No format normalization — the append validator rejects bad observation lines; fixing legacy content is the housekeeping playbook's job.
- No git commits — the store may be git-tracked; commit via `cog_rpc("git", ...)` from a playbook step if your deployment wants history.
