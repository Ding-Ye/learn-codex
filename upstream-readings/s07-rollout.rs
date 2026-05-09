// =============================================================================
//  Upstream reading for s07 — rollout persistence
//  Source: codex-rs/rollout/src/recorder.rs
//          codex-rs/rollout/src/lib.rs
// =============================================================================

/// Source: codex-rs/rollout/src/recorder.rs
pub struct RolloutRecorder {
    tx: Sender<RolloutCmd>,                    // ← async cmd queue
    writer_task: Arc<RolloutWriterTask>,
    rollout_path: PathBuf,
    event_persistence_mode: EventPersistenceMode,
}

pub enum EventPersistenceMode {
    SaveAll,    // record every EventMsg (debug + replay use case)
    None,       // record only chat-relevant items (production user)
}

pub enum RolloutCmd {
    Items(Vec<RolloutItem>),
    Flush(oneshot::Sender<()>),                // forced fsync + ack
    Shutdown,
}

/// Source: codex-rs/rollout/src/lib.rs
pub enum RolloutItem {
    SessionMetadata(SessionMetadata),

    /// One item from the OpenAI Responses API. learn-codex collapses these
    /// into KindAssistantText / KindAssistantToolCall / KindToolResult.
    ResponseItem { from_responses_api: ResponseItem },

    /// Token-budget compaction: when context is close to the model max, the
    /// agent summarises older tool calls/results and replaces them with one
    /// CompactedItems entry. Skipped in learn-codex.
    CompactedItems {
        summary: String,
        dropped_count: usize,
    },

    /// Context updates from external skills / files / MCP.
    ContextUpdate { kind: ContextKind, content: String },

    /// Verbatim EventMsg if SaveAll mode is on.
    EventMessage { event: EventMsg },
}

pub struct SessionMetadata {
    pub id: String,
    pub started_at: SystemTime,
    pub model: String,
    pub cwd: PathBuf,
    pub user: String,
    pub codex_version: String,
    pub git: Option<GitMetadata>,           // branch, head sha, dirty?
}

impl RolloutRecorder {
    pub async fn record_items(&self, items: Vec<RolloutItem>) {
        // Non-blocking send. If channel is full, drop the OLDEST batch and
        // log a warning — never block the main thread.
        let _ = self.tx.send(RolloutCmd::Items(items)).await;
    }

    pub async fn flush(&self) {
        let (tx, rx) = oneshot::channel();
        let _ = self.tx.send(RolloutCmd::Flush(tx)).await;
        let _ = rx.await;
    }
}

/// The actual writer task — runs in its own tokio task.
pub struct RolloutWriterTask {
    rx: Receiver<RolloutCmd>,
    file: File,
    pending_count: usize,
    last_fsync: Instant,
}

impl RolloutWriterTask {
    async fn run(mut self) {
        const FSYNC_INTERVAL: Duration = Duration::from_secs(1);
        const FSYNC_EVERY_N: usize = 16;

        while let Some(cmd) = self.rx.recv().await {
            match cmd {
                RolloutCmd::Items(items) => {
                    for item in items { self.append(item).await; }
                    self.pending_count += 1;
                    if self.pending_count >= FSYNC_EVERY_N
                        || self.last_fsync.elapsed() >= FSYNC_INTERVAL {
                        let _ = self.file.sync_data().await;
                        self.pending_count = 0;
                        self.last_fsync = Instant::now();
                    }
                }
                RolloutCmd::Flush(ack) => {
                    let _ = self.file.sync_data().await;
                    let _ = ack.send(());
                }
                RolloutCmd::Shutdown => break,
            }
        }
    }
}

/// Discovery + resume helpers.
pub fn list_sessions(codex_home: &Path) -> Vec<RolloutMetadata> {
    // walks codex_home/sessions/*.jsonl, parses just the FIRST line (meta)
    // of each, returns sorted DESC by started_at for the resume picker
}

pub fn load_session(rollout_path: &Path) -> Result<LoadedSession, LoadError> {
    // Reads all RolloutItems, recovers from torn last line, returns:
    //   - SessionMetadata
    //   - reconstructed ConversationHistory
    //   - any non-chat ContextUpdates that need to be re-applied
}

// =============================================================================
// Comparison summary
//
//   Concept              | Upstream                        | learn-codex (s07)
//   ---------------------+---------------------------------+------------------------
//   write path           | async writer task + channel     | mutex + sync write
//   batching             | up to N items per write          | one line per write
//   fsync                | periodic (1s or every 16 items) | none
//   channel-full policy  | drop OLDEST batch + warn        | n/a (no channel)
//   compaction           | CompactedItems variant          | (omitted)
//   meta line on resume  | preserved verbatim              | preserved (skip rewrite)
//   torn-line recovery   | recover_from_partial_write       | scan → JSON err → skip
// =============================================================================
