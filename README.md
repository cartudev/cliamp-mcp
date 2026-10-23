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

## Install

The server is a normal program. Any MCP client that can launch a local stdio
command can use it. OpenCode is not required.

Install straight from the repository. There is no release tag; `@main` is the
module version Go fetches from GitHub:

```sh
go install github.com/cartudev/cliamp-mcp@main
```

That puts `cliamp-mcp` in `$(go env GOPATH)/bin` (often `~/go/bin`). Put that
directory on `PATH`, or pass the absolute path in the client config. Go 1.26.10
or newer. `@latest` does not resolve until a `vX.Y.Z` tag exists.

From a checkout of this repo:

```sh
go install .
```

cliamp must already be running, either the TUI or `cliamp --daemon`. The server
does not start the player. If `CLIAMP_CONFIG_DIR` is set, point the server at
the same socket:

```sh
CLIAMP_SOCKET="$CLIAMP_CONFIG_DIR/cliamp.sock" cliamp-mcp
```

## Client config

The process speaks MCP on stdin/stdout and never opens a port. Clients that
use the common `mcpServers` map only need the command:

```json
{
  "mcpServers": {
    "cliamp": {
      "command": "cliamp-mcp"
    }
  }
}
```

Use the absolute path when the client does not inherit your `PATH`.

| Client | Where |
| --- | --- |
| Claude Desktop | `claude_desktop_config.json` → `mcpServers` |
| Cursor | `~/.cursor/mcp.json` or `.cursor/mcp.json` → `mcpServers` |
| VS Code Copilot | MCP config → `servers` / `mcpServers`, command `cliamp-mcp` |
| Anything else | stdio transport, command `cliamp-mcp`, no args, no URL |

OpenCode does not install the program. It only starts a local command. Skip
`go install` and let the first launch fetch the module:

```sh
opencode mcp add cliamp -- go run github.com/cartudev/cliamp-mcp@main
```

That still needs a Go toolchain on `PATH`. There is no OpenCode plugin package
for this repo, and `opencode plugin add` will not build it. The equivalent
config, if you already installed the binary, is:

```json
"cliamp": {
  "type": "local",
  "command": ["cliamp-mcp"]
}
```

## Scope

This is a separate program on purpose. It does not belong in the cliamp core:
the player already publishes the IPC API, and a model client is optional.
