---
title: "s09 · MCP bridge: stdio JSON-RPC to external tool servers"
chapter: 9
slug: s09-mcp-bridge
est_read_min: 12
---

# s09 · MCP bridge: stdio JSON-RPC to external tool servers

> What this teaches: how codex plugs in **third-party tools** the user installs, without recompiling — purely through one standard protocol (MCP).

---

## Problem

s03 gave us `shell`. s04 gave us `apply_patch`. Both live in the codex binary; adding a new tool means editing codex source and shipping a new release.

Real users want different tools: a Notion search tool, a SQL runner, a GitHub API caller. None of those belong in codex's main repo.

**Model Context Protocol** solves this. Users write a tiny stand-alone program (Node, Python, Go — anything), it speaks JSON-RPC on stdio per the MCP spec, replying "I have these tools, each shaped like X". Codex spawns those processes at startup, pulls their tool catalogues, and exposes them to the LLM alongside built-ins. The LLM doesn't know or care whether a tool is built-in or external — `tool_call("notion.search", {...})` flows through the same dispatch.

## Solution

Two layers:

1. **`Conn` (jsonrpc.go)** — generic newline-delimited JSON-RPC 2.0 client. A `Run()` goroutine reads stdout, fans replies into per-id channels, and routes server-pushed notifications onto a separate channel.
2. **`Client` (mcp.go)** — MCP-specific methods on top of `Conn`: `Initialize` (handshake including the **mandatory** `notifications/initialized` ack), `ListTools`, `CallTool(name, args)`.

Tests use an **in-process fake server** — no Node, no `npx`. CI is fully self-contained. To talk to a real MCP server, run `go run ./cmd -- npx -y @modelcontextprotocol/server-everything`.

## How It Works

```
                          stdin (write)
   ┌───────────────┐  ─────────────────▶  ┌─────────────────────┐
   │               │                      │  external MCP       │
   │   learn-codex │                      │  server process     │
   │   (Conn+Client)│                      │  (npx | python | …) │
   │               │  ◀─────────────────  │                     │
   └───────────────┘     stdout (read)    └─────────────────────┘
        │
        │ Conn.Call("initialize", {...}) ──▶ {"jsonrpc":"2.0","id":1,"method":"initialize","params":{...}}\n
        │                                  ◀ {"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":"…"}}}\n
        │ Conn.Notify("notifications/initialized", nil)  ──▶  {"jsonrpc":"2.0","method":"notifications/initialized"}\n
        │ Conn.Call("tools/list", {})       ──▶ ...
        │                                  ◀ {"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"shell","description":"...","inputSchema":{...}},...]}}\n
        │ Conn.Call("tools/call", {name,args}) ─▶ ...
        │                                  ◀ {"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"..."}]}}\n
        ▼
   per-tool Tool implementation: Execute(ctx, args) calls client.CallTool(name, args)
```

Core 30 lines:

```go
// jsonrpc.go: per-id correlation
func (c *Conn) Call(method string, params any) (*Response, error) {
    id := c.nextID.Add(1)
    pb, _ := json.Marshal(params)
    req := Request{JSONRPC: "2.0", ID: id, Method: method, Params: pb}
    line, _ := json.Marshal(req)

    ch := make(chan *Response, 1)
    c.mu.Lock(); c.pending[id] = ch; c.mu.Unlock()
    c.in.Write(append(line, '\n'))
    return <-ch, nil
}

// jsonrpc.go: read loop fans replies into per-id channels
func (c *Conn) Run() error {
    for {
        line, err := c.out.ReadBytes('\n')
        // probe id; if it's a notification (no id), push to notifyCh
        // otherwise look up pending[id] and send the response
        ...
    }
}

// mcp.go: handshake
func (c *Client) Initialize(name, version string) (*InitializeResult, error) {
    resp, _ := c.conn.Call("initialize", InitializeParams{...})
    var result InitializeResult
    json.Unmarshal(resp.Result, &result)
    c.conn.Notify("notifications/initialized", nil)   // ← required by spec!
    return &result, nil
}
```

**4 non-obvious points**:

1. **`notifications/initialized` is mandatory.** Many MCP servers refuse `tools/list` until they see it. Easy to miss.
2. **JSON id types.** When the server echoes our integer id back, the JSON decoder yields `float64`. Cast back to `uint64` for the lookup.
3. **Notifications (no-id requests) need their own channel.** Servers can push progress / log messages spontaneously. If you only listen on response channels you'll deadlock.
4. **stdin write, stdout read, stderr untouched.** MCP uses stdio **only** for protocol messages; server logs must go to stderr or they pollute the JSON stream. Our `cmd/main.go` forwards stderr straight to the user's terminal.

## What Changed (vs. s08)

```diff
+ // jsonrpc.go: generic transport
+ type Request, Response, RPCError struct {…}
+ type Conn struct { in WriteCloser; out *bufio.Reader; pending map[id]chan; notifyCh chan }
+ func (c *Conn) Run() error
+ func (c *Conn) Call(method string, params any) (*Response, error)
+ func (c *Conn) Notify(method string, params any) error
+
+ // mcp.go: MCP-specific layer
+ type ToolDef struct { Name, Description string; InputSchema json.RawMessage }
+ type Client struct{ conn *Conn; server ServerInfo }
+ func (c *Client) Initialize(name, version string) (*InitializeResult, error)
+ func (c *Client) ListTools() ([]ToolDef, error)
+ func (c *Client) CallTool(name string, args json.RawMessage) (*ToolsCallResult, error)
```

## Try It

```bash
cd agents/s09-mcp-bridge

# Self-contained tests (no MCP server required)
go test -v ./...

# Real MCP server (if you have node/npx)
go run ./cmd -- npx -y @modelcontextprotocol/server-everything
# server: server-everything 0.x
#   - echo: ...
#   - add: ...
#   - longRunningOperation: ...
```

## Upstream Source Reading

```upstream:codex-rs/mcp-client/src/lib.rs
// Source: codex-rs/mcp-client/src/lib.rs (excerpt)

pub struct McpClient {
    transport: Arc<dyn Transport>,           // ★ stdio | streamable-http | websocket
    pending: Arc<Mutex<HashMap<RequestId, oneshot::Sender<Response>>>>,
    notifications: broadcast::Sender<Notification>,
    server_info: OnceCell<ServerInfo>,
}

#[async_trait]
pub trait Transport: Send + Sync {
    async fn send(&self, msg: Message) -> Result<()>;
    async fn recv(&self) -> Result<Message>;
}

pub struct StdioTransport {
    stdin: Mutex<ChildStdin>,
    stdout: Mutex<BufReader<ChildStdout>>,
}

impl McpClient {
    pub async fn initialize(&self) -> Result<InitializeResult> {
        let result: InitializeResult = self.call("initialize", InitializeParams {
            protocol_version: "2024-11-05".into(),
            client_info: ClientInfo { name: "codex".into(), version: "0.x".into() },
            capabilities: ClientCapabilities::default(),
        }).await?;
        // Required ack — without this many servers refuse tools/list.
        self.notify("notifications/initialized", ()).await?;
        Ok(result)
    }

    pub async fn list_tools(&self) -> Result<Vec<ToolDef>> { ... }
    pub async fn call_tool(&self, name: &str, args: Value) -> Result<ToolResult> { ... }
}
```

```upstream:codex-rs/core/src/mcp.rs
// Source: codex-rs/core/src/mcp.rs (excerpt)

pub struct McpManager {
    plugins: Arc<PluginsManager>,
}

impl McpManager {
    pub async fn effective_servers(&self) -> Vec<EffectiveMcpServer> {
        // 1. read configured mcp_servers from ~/.codex/config.toml
        // 2. for each: spawn the server process via the configured command,
        //    wrap stdin/stdout in a McpClient, perform initialize + list_tools
        // 3. attach per-tool ToolPluginProvenance{server_name, exposure_level}
        // 4. validate against exposure rules — some servers can be marked
        //    "limited" so only an allowlist of tools surfaces to the model
    }

    pub async fn call_tool(&self, server: &str, tool: &str, args: Value) -> Result<ToolResult> {
        // resolve provenance, dispatch to the right McpClient, surface result
    }
}
```

**Reading notes**:

- **3 transports.** Upstream supports stdio + streamable-http + websocket. We only do stdio. The HTTP transport is SSE-based — very similar in shape to s02's Chat Completions parser; an interesting reader exercise.
- **Provenance tracking.** Upstream records "which server exposed which tool" as a structured `ToolPluginProvenance` for auditability and "limited" exposure modes. We use a flat map.
- **Multi-server.** Upstream codex spawns many MCP servers at once (filesystem, git, web, …). We connect to one — but architecturally it's `[]*Client` away.
- **Rich content blocks.** MCP tool results can include image / resource blocks. Our `ToolContent` only handles text — but the `Type` field is preserved for extension.
- **Session lifecycle.** Upstream handles server crash with reconnect + backoff. We just die. Reader's exercise.

**Read further**: start at `mcp-client/src/lib.rs::McpClient::call`, follow the `Transport` trait into `StdioTransport`, then read `core/src/mcp.rs::McpManager::call_tool` to see how each tool gets registered into the LLM's `tools` array. That's the s03 → s09 → s10 trace.

---

**Next**: s10 binds all nine sessions into a real `codex` binary — cobra-style subcommands, slash commands, `exec` headless mode, `resume` consuming s07's rollout files.
