// =============================================================================
//  Upstream reading for s09 — MCP bridge
//  Source: codex-rs/mcp-client/src/lib.rs
//          codex-rs/core/src/mcp.rs
//          codex-rs/protocol/src/mcp.rs (wire types)
// =============================================================================

/// Source: codex-rs/mcp-client/src/lib.rs
pub struct McpClient {
    transport: Arc<dyn Transport>,           // ← stdio | streamable-http | websocket
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

impl Transport for StdioTransport {
    async fn send(&self, msg: Message) -> Result<()> {
        let line = serde_json::to_string(&msg)?;
        let mut stdin = self.stdin.lock().await;
        stdin.write_all(line.as_bytes()).await?;
        stdin.write_all(b"\n").await?;
        stdin.flush().await?;
        Ok(())
    }
    async fn recv(&self) -> Result<Message> {
        let mut line = String::new();
        let mut stdout = self.stdout.lock().await;
        stdout.read_line(&mut line).await?;
        Ok(serde_json::from_str(&line)?)
    }
}

impl McpClient {
    pub async fn initialize(&self) -> Result<InitializeResult> {
        let result: InitializeResult = self.call("initialize", InitializeParams {
            protocol_version: "2024-11-05".into(),
            client_info: ClientInfo {
                name: "codex".into(),
                version: codex_version().into(),
            },
            capabilities: ClientCapabilities::default(),
        }).await?;
        // ★ Mandatory acknowledgement.
        self.notify("notifications/initialized", ()).await?;
        Ok(result)
    }

    pub async fn list_tools(&self) -> Result<Vec<ToolDef>> {
        let res: ToolsListResult = self.call("tools/list", json!({})).await?;
        Ok(res.tools)
    }

    pub async fn call_tool(&self, name: &str, args: Value) -> Result<ToolResult> {
        self.call("tools/call", ToolsCallParams { name: name.into(), arguments: args }).await
    }

    async fn call<P: Serialize, R: DeserializeOwned>(
        &self,
        method: &str,
        params: P,
    ) -> Result<R> {
        let id = next_id();
        let (tx, rx) = oneshot::channel();
        self.pending.lock().await.insert(id.clone(), tx);
        self.transport.send(Message::Request {
            jsonrpc: "2.0".into(), id, method: method.into(),
            params: serde_json::to_value(params)?,
        }).await?;
        let resp = timeout(REQUEST_TIMEOUT, rx).await??;
        let value = resp.into_result()?;
        Ok(serde_json::from_value(value)?)
    }
}

// -----------------------------------------------------------------------------
// Source: codex-rs/core/src/mcp.rs
// -----------------------------------------------------------------------------

pub struct McpManager {
    plugins: Arc<PluginsManager>,
}

pub struct EffectiveMcpServer {
    pub name: String,
    pub tools: Vec<ToolMetadata>,
    pub server_params: ServerParams,
    pub auth_state: AuthState,
}

pub struct ToolPluginProvenance {
    pub tool_id: String,
    pub server_name: String,
    pub exposure_level: ExposureLevel,         // Full | Limited
}

impl McpManager {
    pub async fn effective_servers(&self) -> Vec<EffectiveMcpServer> {
        // 1. Read configured mcp_servers from ~/.codex/config.toml.
        // 2. For each: spawn the server process via the configured command,
        //    wrap stdin/stdout in a McpClient, perform initialize + list_tools.
        // 3. Attach per-tool ToolPluginProvenance{server_name, exposure_level}.
        // 4. Validate against exposure rules — some servers can be marked
        //    "limited" so only an allowlist of tools surfaces to the model.
        // 5. Cache; refresh on config reload (Op::ReloadUserConfig).
    }

    pub async fn call_tool(&self, server: &str, tool: &str, args: Value) -> Result<ToolResult> {
        let provenance = self.lookup_provenance(server, tool)?;
        let client = self.client_for(server).await?;
        client.call_tool(tool, args).await
    }
}

// =============================================================================
// Comparison summary
//
//   Concept              | Upstream                              | learn-codex (s09)
//   ---------------------+---------------------------------------+----------------------
//   transport            | trait Transport (3 impls)             | Conn over io.WriteCloser+Reader
//   pending registry     | Mutex<HashMap<id, oneshot::Sender>>   | sync.Mutex + map[uint64]chan
//   notification stream  | broadcast::Sender<Notification>       | chan *Request (size 16)
//   handshake ack        | notifications/initialized             | Notify("notifications/initialized")
//   tool catalog         | Vec<ToolDef> + ToolPluginProvenance   | []ToolDef
//   exposure modes       | Full | Limited                        | (omitted)
//   reconnect            | restart-with-backoff on crash         | (omitted)
//   multi-server         | spawn N, route by provenance          | one client per Conn
// =============================================================================
