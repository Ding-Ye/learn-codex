// =============================================================================
//  Upstream reading for s02 — model client
//  Source: codex-rs/core/src/client.rs
//          codex-rs/model-provider/src/lib.rs
// =============================================================================

// -----------------------------------------------------------------------------
// 1. The provider trait — Codex's polymorphism over LLM backends.
// -----------------------------------------------------------------------------

/// Source: codex-rs/model-provider/src/lib.rs
///
/// ★ Our `Provider` interface in agents/s02-model-client/provider.go is the
/// direct analogue of this trait.
pub trait ModelProvider: Send + Sync {
    fn capabilities(&self) -> ProviderCapabilities;
    fn auth(&self) -> &AuthProvider;

    /// The streaming completion call.
    fn stream(&self, request: CompletionRequest) -> ResponseStream;
}

pub fn create_model_provider(kind: ProviderKind) -> SharedModelProvider {
    match kind {
        ProviderKind::OpenAI(opts)   => Arc::new(OpenAIChatProvider::new(opts)),
        ProviderKind::Bedrock(opts)  => Arc::new(BedrockProvider::new(opts)),
        ProviderKind::Ollama(opts)   => Arc::new(OllamaProvider::new(opts)),
        ProviderKind::LMStudio(opts) => Arc::new(LMStudioProvider::new(opts)),
        ProviderKind::CustomHTTP(opts) => Arc::new(CustomHTTPProvider::new(opts)),
    }
}

// -----------------------------------------------------------------------------
// 2. ModelClient — the top-level client used by core/codex_thread.
// -----------------------------------------------------------------------------

/// Source: codex-rs/core/src/client.rs
pub struct ModelClient {
    auth: Arc<AuthManager>,
    provider: ModelProvider,
    thread_id: String,
    session_id: String,
    ws_fallback: Arc<Mutex<WebSocketFallbackState>>,
}

pub struct ModelClientSession {
    client: Arc<ModelClient>,
    ws_connection: Option<WebSocketConnection>,
    turn_state_token: Option<String>,
}

impl ModelClient {
    /// ★ This is what s02's Codex.runTurn() effectively calls.
    pub async fn stream(
        &self,
        request: ResponsesApiRequest,
    ) -> Result<impl Stream<Item = ResponseItem>, ClientError> {
        // 1. Try WebSocket connection (sticky to a backend instance via
        //    x-codex-turn-state header so streaming responses don't get
        //    rerouted mid-flight).
        // 2. On WS failure: HTTP SSE fallback.
        // 3. Parse Responses API events into ResponseItem objects.
        // 4. Maintain per-session state: thread_id, session_id, turn_state_token.

        // (~120 LOC of error handling and routing trimmed)
    }
}

// -----------------------------------------------------------------------------
// 3. ResponseItem — the Responses API atom.
// -----------------------------------------------------------------------------

/// Source: codex-rs/protocol/src/responses_api.rs
///
/// ★ Our learn-codex equivalent is `ProviderEvent`, but ours is
///   { ProvText, ProvToolCall, ProvDone, ProvError } — fewer variants because
///   Chat Completions doesn't expose Reasoning blocks separately.
pub enum ResponseItem {
    Text { id: String, delta: String },
    ToolUse { id: String, name: String, input: serde_json::Value },
    Reasoning { id: String, delta: String },     // ← not in Chat Completions
    Done { finish_reason: String },
}

// -----------------------------------------------------------------------------
// 4. Stream demuxing — the part that actually corresponds to our SSE parser.
// -----------------------------------------------------------------------------

/// Source: codex-rs/core/src/responses_stream.rs (conceptual)
///
/// Upstream parses Responses API SSE which is shape-similar to Chat Completions
/// SSE but with named events (response.text.delta, response.tool_use.input, etc.)
/// instead of {choices:[{delta:{...}}]}.
async fn parse_responses_sse<R: AsyncBufRead>(reader: R) -> impl Stream<Item = ResponseItem> {
    // for each `event: <name>` + `data: <json>` pair:
    //   - response.text.delta → ResponseItem::Text { delta }
    //   - response.tool_use.input → accumulate by id, emit ToolUse on completion
    //   - response.reasoning.delta → ResponseItem::Reasoning { delta }
    //   - response.done → ResponseItem::Done
}

// =============================================================================
// Comparison summary
//
//   Concept                | Upstream                       | learn-codex
//   -----------------------+-------------------------------+----------------------
//   provider trait         | model-provider::ModelProvider  | provider.go::Provider
//   transport              | WS primary + HTTP fallback     | HTTP SSE only
//   wire protocol          | OpenAI Responses API           | OpenAI Chat Completions
//   tool-call atom         | ResponseItem::ToolUse{id,name,input} | ToolCall{ID,Function{Name,Args}}
//   reasoning streams      | ResponseItem::Reasoning        | (omitted)
//   sticky routing         | x-codex-turn-state header      | (omitted)
//   per-session state      | thread_id + session_id         | (omitted; runs are stateless)
// =============================================================================
