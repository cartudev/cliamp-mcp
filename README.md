# cliamp-mcp

Local MCP server for a running [cliamp](https://github.com/bjarneo/cliamp).

It speaks MCP over stdio and forwards every call to cliamp's own IPC socket
(`~/.config/cliamp/cliamp.sock`, mode `0600`). Playback, the queue and the
providers stay inside cliamp. Nothing is exposed on the network: the socket
only exists while cliamp runs, and only the owning user can connect to it.

## Tools

| Tool | What it does |
| --- | --- |
| `cliamp_state` | Current track, playback state, position, volume |
| `cliamp_capabilities` | Operations the running instance accepts |
| `cliamp_queue` | Loaded playlist. `query` filters that list; `offset`/`limit` page it. Cover art is omitted |
| `cliamp_providers` | Configured providers |
| `cliamp_search` | Provider search. If the provider has none, filters the playlist already loaded |
| `cliamp_call` | Any IPC operation (`operation`, optional `params`) |

`cliamp_call` covers the rest of the IPC surface: `next`, `pause`, `seek`,
`provider.load`, `queue.enqueue`, and so on. Ask `cliamp_capabilities` for the
names and parameters. A submitted job is polled until it finishes.

## Build

```sh
go build -o ~/.local/bin/cliamp-mcp .
```

Requires Go 1.26.10 or newer. cliamp must already be running (the TUI or
`cliamp --daemon`). Override the socket with `CLIAMP_SOCKET` when
`CLIAMP_CONFIG_DIR` is set.

## OpenCode

```json
"cliamp": {
  "type": "local",
  "command": ["/home/you/.local/bin/cliamp-mcp"],
  "enabled": true
}
```

## Scope

This is a separate program on purpose. It does not belong in the cliamp core:
the player already publishes the IPC API, and a model client is optional.
