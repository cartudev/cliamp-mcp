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
command can use it. OpenCode is not required. cliamp must already be running
(the TUI or `cliamp --daemon`). This program does not start the player.

No Go toolchain is required. Download the release file for the machine, name it
`cliamp-mcp`, and put it on `PATH`. Check `checksums.txt` on the same release.

| Machine | File |
| --- | --- |
| Linux x86_64 | `cliamp-mcp-linux-amd64` |
| Linux arm64 | `cliamp-mcp-linux-arm64` |
| macOS Intel | `cliamp-mcp-darwin-amd64` |
| macOS Apple silicon | `cliamp-mcp-darwin-arm64` |
| Windows x86_64 | `cliamp-mcp-windows-amd64.exe` |
| Windows arm64 | `cliamp-mcp-windows-arm64.exe` |

Linux or macOS, x86_64. On Apple silicon or Linux arm64, change the filename.

```sh
curl -fsSL -o ~/.local/bin/cliamp-mcp \
  https://github.com/cartudev/cliamp-mcp/releases/latest/download/cliamp-mcp-linux-amd64
chmod +x ~/.local/bin/cliamp-mcp
```

```sh
curl -fsSL -o ~/.local/bin/cliamp-mcp \
  https://github.com/cartudev/cliamp-mcp/releases/latest/download/cliamp-mcp-darwin-arm64
chmod +x ~/.local/bin/cliamp-mcp
```

Windows, from PowerShell. Change `amd64` to `arm64` on an ARM machine. Put that
directory on `PATH`, or pass the full path in the client config.

```powershell
$dir = "$env:LOCALAPPDATA\cliamp-mcp"
New-Item -ItemType Directory -Force -Path $dir | Out-Null
Invoke-WebRequest `
  -Uri https://github.com/cartudev/cliamp-mcp/releases/latest/download/cliamp-mcp-windows-amd64.exe `
  -OutFile "$dir\cliamp-mcp.exe"
```

If `CLIAMP_CONFIG_DIR` is set, the server must use the same socket:

```sh
CLIAMP_SOCKET="$CLIAMP_CONFIG_DIR/cliamp.sock" cliamp-mcp
```

On Windows the variable is `CLIAMP_SOCKET`.

### Build instead of downloading

Go 1.26.10 or newer. `@latest` follows the newest `vX.Y.Z` tag. `@main` follows
the branch. The binary lands in `$(go env GOPATH)/bin`, often `~/go/bin`.

```sh
go install github.com/cartudev/cliamp-mcp@latest
go install github.com/cartudev/cliamp-mcp@main
```

From a checkout:

```sh
go install .
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

Use the absolute path when the client does not inherit your `PATH`. On Windows
the command is `cliamp-mcp.exe`.

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
