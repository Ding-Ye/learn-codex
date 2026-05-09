---
title: "s09 · MCP 桥：stdio JSON-RPC 调用外部工具服务器"
chapter: 9
slug: s09-mcp-bridge
est_read_min: 12
---

# s09 · MCP 桥：stdio JSON-RPC 调用外部工具服务器

> 教什么：codex 怎么把"用户安装的、第三方写的工具"接进来——不靠重新编译，纯靠一个标准协议（MCP）。

---

## Problem / 问题

s03 给我们一个 `shell` 工具，s04 给我们一个 `apply_patch` 工具。它们都在 codex 的二进制里，加新工具必须改 codex 源码、重新发布。

但用户场景五花八门：有人想要一个查 Notion 的工具、一个跑 SQL 的工具、一个调 GitHub API 的工具。每一个都进 codex 主仓库不现实。

**Model Context Protocol** 解决这个问题：用户写一个独立的小程序（Node / Python / Go 都行），按 MCP spec 在 stdio 上接收 JSON-RPC，回复"我有这些工具，每个工具长这样"。codex 启动时 spawn 这些程序，把它们暴露的工具和内置工具一起放进 LLM 的 tools 列表。LLM 不知道也不关心一个工具是内置的还是外部的——`tool_call("notion.search", {...})` 走同一个分发路径。

## Solution / 解决方案

两层：

1. **`Conn`（jsonrpc.go）** —— 通用的 newline-delimited JSON-RPC 2.0 客户端。一个 `Run()` goroutine 读 stdout，按 id 把响应分发到 per-call channel，server 主动推的 notification 走另一个 channel。
2. **`Client`（mcp.go）** —— 在 `Conn` 之上塞 MCP 的具体方法：`Initialize` 做握手（包含**必须的** `notifications/initialized` 通知），`ListTools` 拉工具清单，`CallTool(name, args)` 调用一个工具。

测试用一个**进程内 fake server**——不需要装 node/npx，CI 100% 自包含。要试真实 server，跑 `go run ./cmd -- npx -y @modelcontextprotocol/server-everything`。

## How It Works / 工作原理

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

核心 30 行：

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

**4 个非显然之处**：

1. **`notifications/initialized` 是必需的**——很多 MCP server 在收到这条之前不会响应 `tools/list`。spec 写了但容易漏。
2. **JSON id 类型**——服务端往回 echo id 时，整数会被 JSON 解码成 `float64`。我们 cast 回 uint64 来 lookup。
3. **notification（没 id 的请求）走单独 channel**——server 可能主动推 progress / log。如果你只 listen 响应 channel 会卡住。
4. **stdin 写、stdout 读、stderr 不动**——MCP 协议**只**用 stdio 传协议消息；server 的 log 应该走 stderr，不要污染 stdout 那条 JSON 流。我们的 `cmd/main.go` 把 stderr 直接转发给用户终端。

## What Changed / 与 s08 的变化

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

## Try It / 动手试一试

```bash
cd agents/s09-mcp-bridge

# Self-contained tests (no MCP server required)
go test -v ./...

# 真实 MCP server（如果你有 node/npx）
go run ./cmd -- npx -y @modelcontextprotocol/server-everything
# server: server-everything 0.x
#   - echo: ...
#   - add: ...
#   - longRunningOperation: ...
```

## Upstream Source Reading / 上游源码阅读

```upstream:codex-rs/mcp-client/src/lib.rs
// Source: codex-rs/mcp-client/src/lib.rs (节选)

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
// Source: codex-rs/core/src/mcp.rs (节选)

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

**对照阅读要点**：

- **3 种 Transport**：上游有 stdio + streamable-http + websocket。学习版只做 stdio。HTTP transport 用 SSE 流，跟 s02 的 ChatCompletions 解析很像，是个有趣的延伸练习。
- **provenance 追踪**：上游把"哪个 server 暴露了哪个 tool"做成结构化记录（`ToolPluginProvenance`），用于审计和"限制级"暴露。学习版直接 flat map。
- **multi-server**：上游一个 codex 同时 spawn 多个 MCP server（filesystem、git、web、…）。学习版接一个就停——但 architecture 上加 `[]*Client` 即可扩展。
- **rich content blocks**：MCP 工具结果可以含 image / resource block。学习版 `ToolContent` 只看 text——但保留 `Type` 字段方便扩展。
- **session lifecycle**：上游做了 server crash 的 reconnect、restart-with-backoff。学习版崩了就是崩了——一个 readers' exercise。

**想读更多**：从 `mcp-client/src/lib.rs::McpClient::call` 入手，跟着 `Transport` trait 进 `StdioTransport`，再看 `core/src/mcp.rs::McpManager::call_tool` 怎么把每个 tool 注册进 LLM tools 列表里——这条线是 s03 → s09 → s10 的代码地图。

---

**下一节预告**：s10 把所有九节装订成一个真的 `codex` 二进制——cobra-style 子命令、slash commands、`exec` headless 模式、`resume` 复用 s07 的 rollout。
