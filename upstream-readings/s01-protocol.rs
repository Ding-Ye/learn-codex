// =============================================================================
//  Upstream reading for s01 — the protocol
//  Source files quoted: codex-rs/protocol/src/protocol.rs
//                       codex-rs/core/src/codex_thread.rs
//  Upstream commit: openai/codex@main (see ~2026-05-09 snapshot)
//
//  How to use this file:
//   - The Rust below is a faithful but trimmed excerpt of the upstream source.
//   - We strip serde derives, tracing context plumbing, and rare variants —
//     anything that obscures the shape we care about in s01.
//   - Comments prefixed with `// ★` are our annotations, not upstream's.
// =============================================================================

// -----------------------------------------------------------------------------
// 1. The Op enum — what the frontend sends INTO the agent.
// -----------------------------------------------------------------------------

/// Source: codex-rs/protocol/src/protocol.rs
///
/// ★ Upstream has ~20 variants. We list the 6 that recur across our 10 chapters.
pub enum Op {
    /// User typed text at the prompt.
    UserInput { text: String },

    /// User responded to a prior `ExecApprovalRequest`.
    /// ★ Used in s05 onwards. The id field correlates with the request.
    ExecApproval { id: String, approved: bool },

    /// User responded to a prior `PatchApprovalRequest`.
    PatchApproval { id: String, approved: bool },

    /// Cancel any in-flight turn. The loop emits TurnComplete{cancelled=true}.
    Interrupt,

    /// Drain remaining events and close the bus.
    Shutdown,

    /// Re-read ~/.codex/config.toml; apply changes mid-session.
    /// ★ We don't model this in learn-codex.
    ReloadUserConfig,
    // ~14 more variants: AddMcpServer, EditPrompt, SwitchProfile, …
}

// -----------------------------------------------------------------------------
// 2. The EventMsg enum — what the agent emits OUT.
// -----------------------------------------------------------------------------

/// Source: codex-rs/protocol/src/protocol.rs
///
/// ★ Upstream has ~30 variants. We keep the 13 that map directly to behaviour
///   we'll build in s01..s10. The others (ContextCompacted, McpListUpdated,
///   FocusComposer, …) are TUI conveniences or v.next features.
pub enum EventMsg {
    // ---- turn lifecycle (s01) ----------------------------------------------
    TurnStarted   { turn_id: String },
    AgentMessage  { turn_id: String, text: String },
    TurnComplete  { turn_id: String },
    ShutdownComplete,

    // ---- shell exec tool (s03) ---------------------------------------------
    ExecCommandBegin { id: String, command: Vec<String>, cwd: std::path::PathBuf },
    ExecOutputDelta  { id: String, stream: Stream, bytes: Vec<u8> },
    ExecCommandEnd   { id: String, exit_code: i32 },

    // ---- approval gate (s05) -----------------------------------------------
    ExecApprovalRequest  { id: String, command: Vec<String>, reason: String },
    PatchApprovalRequest { id: String, path: std::path::PathBuf, patch_text: String },

    // ---- patch tool (s04) --------------------------------------------------
    PatchApplyBegin { id: String, path: std::path::PathBuf },
    PatchApplyEnd   { id: String, success: bool },

    // ---- generic error -----------------------------------------------------
    Error { message: String },

    // ~17 more variants: ContextCompacted, McpListUpdated, FocusComposer, …
}

pub enum Stream { Stdout, Stderr }

// -----------------------------------------------------------------------------
// 3. Submission — the wrapper that travels through the queue.
// -----------------------------------------------------------------------------

/// Source: codex-rs/protocol/src/protocol.rs
pub struct Submission {
    pub id: SubmissionId,
    pub op: Op,

    /// W3C trace context (RFC 9457). Upstream propagates this through every
    /// EventMsg so distributed tracing works across the app-server boundary.
    /// ★ Skipped in learn-codex.
    pub trace_context: Option<TracingContext>,
}

// -----------------------------------------------------------------------------
// 4. CodexThread — the loop's public face.
// -----------------------------------------------------------------------------

/// Source: codex-rs/core/src/codex_thread.rs
///
/// ★ This is what s01's Codex struct is modelled on. The Go version's
///   `Submit(op) -> id` and `Events() <-chan EventMsg` map 1:1 onto Rust's
///   `submit()` / `next_event()`.
pub struct CodexThread {
    pub(crate) codex: Codex,
    pub(crate) session_source: SessionSource,
    rollout_path: Option<std::path::PathBuf>,
}

impl CodexThread {
    /// Enqueue an Op. Returns the assigned submission id.
    /// ★ async fn upstream; goroutine + channel send in Go.
    pub async fn submit(&self, op: Op) -> SubmissionId { /* … */ }

    /// Pop the next EventMsg from the bus.
    /// ★ async fn upstream; <-c.Events() in Go.
    pub async fn next_event(&self) -> EventMsg { /* … */ }

    /// Variant carrying a W3C trace context.
    pub async fn submit_with_trace(
        &self,
        op: Op,
        w3c_trace: TracingContext,
    ) -> SubmissionId { /* … */ }
}

// -----------------------------------------------------------------------------
// Comparison summary
//
//   Concept       | Upstream (Rust)               | learn-codex (Go)
//   --------------+-------------------------------+-------------------------------
//   tagged union  | enum Op { … }                 | interface{ isOp() } + types
//   queue         | tokio::mpsc::Sender<Sub>      | chan Submission
//   event stream  | tokio::mpsc::Receiver<Ev>     | chan EventMsg
//   async         | tokio runtime                 | goroutine
//   trace context | W3C TracingContext            | (omitted)
//   wire format   | serde JSON / msgpack          | (in-process only)
//
// The shape is identical. Everything else in codex — model client, exec,
// approval, rollout, MCP — extends this same primitive.
// =============================================================================
