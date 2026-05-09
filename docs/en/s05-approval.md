---
title: "s05 · approval policy + EvExecApprovalRequest round-trip"
chapter: 5
slug: s05-approval
est_read_min: 10
---

# s05 · approval policy + EvExecApprovalRequest round-trip

> What this teaches: wrap s03's shell and s04's apply_patch in a "user-must-allow-first" gate.

---

## Problem

The LLM occasionally wants to run `rm -rf ~`. Even if it's right *most* of the time, "occasionally" is too expensive. We need an ask-before-act mechanism:

- Different users have different risk tolerance (CI pipeline vs interactive), so policy is configurable.
- We can't ask for *every* call (constant interruptions are awful), so trusted commands skip the prompt.
- Once the user denies several times in a row, we should bail out instead of looping forever.

## Solution

`Gate`:

- Holds an `ApprovalPolicy` (Never / OnFailure / OnRequest / UnlessTrusted / Granular).
- `Check(ctx, req, emit)` calls `emit(req)` only when an approval is needed (the agent loop turns that into `EvExecApprovalRequest`), then blocks on a channel until `Resolve(id, approved)` is called.
- Counter pair: ≥3 consecutive denials or ≥10 total → set `Denied3x`; the loop reads this and emits `EvTurnComplete{Cancelled:true}`.
- ctx cancellation == deny.

## How It Works

```
agent loop                  Gate                          frontend
──────────                  ────                          ────────
exec_tool_about_to_run ─▶ Check(ctx, req, emit)
                              │
                              │ policy switch:
                              │   ApprovalNever      → return Allow
                              │   UnlessTrusted/Granular:
                              │      if Trusted(cmd) → return Allow
                              │   otherwise:
                              │      pending[req.ID] = ch  (chan Decision)
                              │      emit(req) ───────────────────▶ EvExecApprovalRequest
                              │      <-ch          (block)
                                                                │
                                                                │ user types y/n
                                                                ▼
                                                    Op::ExecApproval{ID, Approved}
                              ◀──────────────  Resolve(req.ID, true|false) ◀──
                              │      ch <- Allow|Deny
                              ▼
                          afterDecision: bump counters; set Denied3x if exceeded
                              │
                          ◀── return decision
```

Core 30 lines from [`agents/s05-approval/gate.go`](https://github.com/Ding-Ye/learn-codex/blob/main/agents/s05-approval/gate.go):

```go
func (g *Gate) Check(ctx context.Context, req Request, emit func(Request)) Decision {
    switch g.Policy {
    case ApprovalNever: return DecisionAllow
    case ApprovalUnlessTrusted, ApprovalGranular:
        if g.Trusted != nil {
            if ok, _ := g.Trusted(req.Command); ok { return DecisionAllow }
        }
    }
    ch := make(chan Decision, 1)
    g.mu.Lock(); g.pending[req.ID] = ch; g.mu.Unlock()
    emit(req)
    select {
    case d := <-ch:
        g.afterDecision(d); return d
    case <-ctx.Done():
        g.mu.Lock(); delete(g.pending, req.ID); g.mu.Unlock()
        return DecisionDeny
    }
}
```

**4 non-obvious points**:

1. **emit only fires when we'll actually ask.** Never and Trusted-allowed paths never bother the frontend.
2. **`pending` is a map[id]chan.** Concurrent approvals across different tool calls don't interfere; one earlier call may still be waiting while the next one fires.
3. **ctx cancellation = deny.** A pending approval during `OpInterrupt` / `OpShutdown` should unwind cleanly.
4. **Counters use atomics + a bool flag.** The counters themselves are atomic; `Denied3x` is read non-atomically — only `afterDecision` writes it, and only the loop goroutine reads it.

## What Changed (vs. s04)

```diff
+ type ApprovalPolicy int    // Never | OnFailure | OnRequest | UnlessTrusted | Granular
+ type Decision int          // Allow | Deny
+ type Request struct { ID string; Command []string; Path, Reason string }
+ type Trusted func(cmd []string) (allowed bool, reason string)
+
+ type Gate struct {
+     Policy ApprovalPolicy
+     Trusted Trusted
+     Denied3x bool
+     // …pending map, counters
+ }
+ func (g *Gate) Check(ctx, req, emit) Decision
+ func (g *Gate) Resolve(id string, approved bool) error
```

s06 will populate the `Trusted` function pointer from a real DSL.

## Try It

```bash
cd agents/s05-approval
go run ./cmd
# you'll see:
# [approve?] command: [ls /tmp] reason=""
# y/n > y
# decision: Allow ...

go test -v ./...
```

## Upstream Source Reading

```upstream:codex-rs/core/src/codex_thread.rs
// Source: codex-rs/core/src/codex_thread.rs (approval branch — excerpt)

pub enum AskForApproval {
    Never, OnFailure, OnRequest, UnlessTrusted, Granular,
}

impl CodexThread {
    async fn maybe_ask_for_approval(
        &self,
        kind: ToolKind,
        command: &[String],
    ) -> ApprovalDecision {
        match self.config.ask_for_approval {
            AskForApproval::Never => ApprovalDecision::Allow,
            AskForApproval::UnlessTrusted | AskForApproval::Granular => {
                if let Some(rule) = self.exec_policy.classify(command) {
                    if rule.is_allow() {
                        return ApprovalDecision::Allow;
                    }
                }
                self.emit_and_wait(kind, command).await
            }
            AskForApproval::OnRequest => self.emit_and_wait(kind, command).await,
            AskForApproval::OnFailure => ApprovalDecision::AllowOnce, // retry-only path
        }
    }

    async fn emit_and_wait(&self, kind: ToolKind, command: &[String]) -> ApprovalDecision {
        let id = SubmissionId::new();
        let (tx, rx) = oneshot::channel();
        self.pending_approvals.lock().await.insert(id, tx);
        self.events.send(EventMsg::ExecApprovalRequest { id, command: command.to_vec(), reason: ... }).await;
        rx.await.unwrap_or(ApprovalDecision::Deny)
    }

    fn record_denial(&self) {
        let consec = self.consecutive_denials.fetch_add(1, Ordering::Relaxed) + 1;
        let total  = self.total_denials.fetch_add(1, Ordering::Relaxed) + 1;
        if consec >= 3 || total >= 10 {
            self.interrupt_turn();
        }
    }
}
```

**Reading notes**:

- **`OnFailure` is real.** Upstream's OnFailure means "run first; if it failed, ask whether to retry". We define the enum but skip the retry path — left as a reader exercise.
- **`exec_policy.classify` returns a `Rule`.** Upstream's trusted check goes through the execpolicy DSL (s06). We leave room with a `Trusted` function pointer.
- **`oneshot` channels.** Upstream uses tokio oneshot rather than mpsc. Go's buffered `chan Decision` of size 1 is the equivalent.
- **`record_denial` fires `interrupt_turn`.** Upstream actively injects an `Op::Interrupt` into the current turn. We only set `Denied3x` and let the caller decide.

**Read further**: start at `codex_thread.rs::run_turn` and follow into `tool_dispatch::shell`'s entry point — see how the approval gate wraps around sandboxing and outside execpolicy. That's the s05 → s06 → s08 source map.

---

**Next**: s06 implements the `Trusted` function pointer as a real DSL — read `.rules` files, prefix-match commands, decide `Allow / Prompt / Forbidden`.
