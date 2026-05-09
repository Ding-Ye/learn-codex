// =============================================================================
//  Upstream reading for s05 — approval policy
//  Source: codex-rs/protocol/src/protocol.rs   (AskForApproval enum)
//          codex-rs/core/src/codex_thread.rs   (approval branch)
// =============================================================================

/// Source: codex-rs/protocol/src/protocol.rs
pub enum AskForApproval {
    /// Run everything without asking.
    Never,
    /// Try once; if non-zero exit, prompt to retry.
    OnFailure,
    /// Always prompt before running.
    OnRequest,
    /// Skip the prompt for commands marked `allow` by execpolicy.
    UnlessTrusted,
    /// Like UnlessTrusted but with per-rule reasons in the prompt.
    Granular,
}

/// Source: codex-rs/core/src/codex_thread.rs (approval branch)
impl CodexThread {
    async fn maybe_ask_for_approval(
        &self,
        kind: ToolKind,
        command: &[String],
    ) -> ApprovalDecision {
        match self.config.ask_for_approval {
            AskForApproval::Never => ApprovalDecision::Allow,

            AskForApproval::UnlessTrusted | AskForApproval::Granular => {
                // ★ The "trusted check" is delegated to execpolicy (s06).
                if let Some(rule) = self.exec_policy.classify(command) {
                    if rule.is_allow() {
                        return ApprovalDecision::Allow;
                    }
                }
                self.emit_and_wait(kind, command).await
            }

            AskForApproval::OnRequest => self.emit_and_wait(kind, command).await,

            AskForApproval::OnFailure => ApprovalDecision::AllowOnce,
        }
    }

    async fn emit_and_wait(&self, kind: ToolKind, command: &[String]) -> ApprovalDecision {
        let id = SubmissionId::new();
        let (tx, rx) = oneshot::channel();
        self.pending_approvals.lock().await.insert(id.clone(), tx);

        self.events
            .send(EventMsg::ExecApprovalRequest {
                id: id.clone(),
                command: command.to_vec(),
                reason: self.classify_reason(command),
            })
            .await;

        // ★ Frontend will eventually send Op::ExecApproval { id, approved },
        //   the loop calls `record_approval_response(id, approved)` which
        //   `tx.send(decision)`s on the oneshot.
        rx.await.unwrap_or(ApprovalDecision::Deny)
    }

    fn record_denial(&self) {
        let consec = self.consecutive_denials.fetch_add(1, Ordering::Relaxed) + 1;
        let total  = self.total_denials.fetch_add(1, Ordering::Relaxed) + 1;
        if consec >= MAX_CONSECUTIVE_DENIALS || total >= MAX_TOTAL_DENIALS {
            self.interrupt_turn();
        }
    }
}

const MAX_CONSECUTIVE_DENIALS: u64 = 3;
const MAX_TOTAL_DENIALS: u64 = 10;

// =============================================================================
// Comparison summary
//
//   Concept                | Upstream                          | learn-codex (s05)
//   -----------------------+-----------------------------------+----------------------
//   policy enum            | AskForApproval (5 variants)       | ApprovalPolicy (5 variants)
//   pending registry       | Mutex<HashMap<Id, oneshot::Sender>> | sync.Mutex + map[id]chan
//   wake-up                | oneshot::Sender ←→ Receiver       | buffered chan(1)
//   trusted check          | execpolicy::classify              | Trusted func pointer (s06 fills)
//   denial counters        | atomic<u64> consecutive + total   | atomic.Int64 + atomic.Int64
//   counter exceeded       | self.interrupt_turn()             | sets Denied3x flag for caller
//   OnFailure              | retry-once, then ask              | (defined, not implemented)
// =============================================================================
