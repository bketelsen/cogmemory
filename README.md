# cogmemory

Cog memory service is a concurrent-safe JSON-RPC 2.0 daemon for agent memory files.
It listens on a Unix Domain Socket and centralizes file operations so multiple
agents can read and write memory without corrupting shared state.

## Why it exists

Agent memory files need atomic writes, append safety, search, indexing, stats,
and role-based access control. `cogmemory` packages those operations as a small
standalone Go service that can be installed per user and managed by systemd.

## Features

- JSON-RPC 2.0 over Unix Domain Socket
- Methods: `read`, `write`, `append`, `patch`, `search`, `stats`, `l0index`,
  `list`, `open_actions`, `domains.list`, `domains.get`, `health`
- Canonical domain registry loaded from `<memory_root>/domains.yml`
  (hot-reload on file change). The daemon exposes it via `domains.list` /
  `domains.get`, and `open_actions` uses it to enumerate action-items
  files instead of inferring them from leaf directory names. Pass
  `{"domain": "<id>"}` to `open_actions` to scope the scan to a single
  domain. Writes that land under a declared domain but use an undeclared
  file basename emit a warning log line (hygiene signal, not blocking).
- File locking and atomic writes
- Glob-pattern RBAC by role
- Optional systemd notify/watchdog support

## Configuration

By default the binary reads `~/.config/cogmemory/config.yml`. Override it with:

```bash
cogmemory --config /path/to/config.yml
```

Example:

```yaml
socket_path: /tmp/cogmemory.sock
memory_root: /home/me/.local/share/cogmemory/memory
log_level: info
watchdog_sec: 30
rbac:
  roles:
    admin:
      - pattern: "**"
        read: true
        write: true
    researcher:
      - pattern: "**"
        read: true
        write: false
    coder:
      - pattern: "projects/**"
        read: true
        write: true
      - pattern: "**"
        read: true
        write: false
```

Required fields: `memory_root`. If omitted, `socket_path` defaults to
`$STATE_HOME/memory.sock` when `STATE_HOME` is set, otherwise `/tmp/cogmemory.sock`.

## Install

```bash
make test
make install-versioned
make install-service
systemctl --user enable --now cogmemory
```

Or install only the service unit:

```bash
deploy/install-service.sh
```

The service expects:

- Binary: `~/.local/bin/cogmemory`
- Config: `~/.config/cogmemory/config.yml`
- Optional env file: `~/.config/cogmemory/.env`

## Environment

- `XDG_CONFIG_HOME`: used by the default config path when set
- `STATE_HOME`: used for the default socket path when `socket_path` is omitted
- `NOTIFY_SOCKET`: set by systemd for readiness/watchdog notifications

## Attribution

The memory conventions, tier architecture (hot/warm/glacier), L0/L1/L2 retrieval protocol,
pipeline skills (reflect, housekeeping, foresight, evolve), and observation format implemented
by this service are based on **[Cog](https://github.com/marciopuga/cog)** by
[Marcio Puga](https://github.com/marciopuga) — a plain-text cognitive architecture for
AI agents. Cog defines the rules; this service provides the concurrent-safe filesystem
daemon that enforces them across multiple agents sharing a single memory tree.
