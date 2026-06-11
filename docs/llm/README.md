# LLM Integration Docs (SSOT)

Single source of truth for hosting cog memory in any LLM app. The daemon
defines the contract; these docs live next to the code that enforces it —
PRs that change an envelope must change RPC.md in the same diff.

| Doc | What it is |
|---|---|
| [INTEGRATION.md](INTEGRATION.md) | How to host cog: wire protocol, tool surface, session-brief injection, skills hosting, failure modes |
| [CONVENTIONS.md](CONVENTIONS.md) | The canonical conventions text every host injects into sessions (successor to cog's CLAUDE.md) |
| [RPC.md](RPC.md) | Per-method reference: params, envelopes, errors — extracted from the Go structs |
| [skills/](skills/) | Canonical skill playbooks (cog, reflect, housekeeping, foresight, evolve, history) with marked HOST-ADAPTATION points |

Hosts vendor `skills/` and the CONVENTIONS text rather than forking them.
Reference host implementation: [ytsejam](https://github.com/bketelsen/ytsejam)
(`server/src/cog/`, `server/src/tools/cog.ts`, `server/skills/`).
