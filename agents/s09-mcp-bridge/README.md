# s09 · MCP bridge (stdio JSON-RPC)

Spawn an external Model Context Protocol server on stdio and bridge its tools into the agent's tool registry.

- `Conn` — newline-delimited JSON-RPC 2.0 over (stdin, stdout). One read goroutine fans replies into per-id channels and notifications into a separate channel.
- `Client` — MCP shapes on top: `Initialize` (handshake + acknowledged), `ListTools`, `CallTool(name, args)`.

The unit tests use an in-process fake JSON-RPC server (no `npx` required, so CI can pass without Node installed).

## Run

```bash
cd agents/s09-mcp-bridge

# In-process tests
go test -v ./...

# Talk to a real MCP server (if you have node/npx):
go run ./cmd -- npx -y @modelcontextprotocol/server-everything
```

## Upstream

- [`codex-rs/mcp-client/`](https://github.com/openai/codex/tree/main/codex-rs/mcp-client).
- [`codex-rs/core/src/mcp.rs`](https://github.com/openai/codex/blob/main/codex-rs/core/src/mcp.rs).
- [`upstream-readings/s09-mcp.rs`](../../upstream-readings/s09-mcp.rs).
