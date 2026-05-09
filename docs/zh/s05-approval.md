---
title: "s05 · approval：审批策略 + EvExecApprovalRequest 往返"
chapter: 5
slug: s05-approval
est_read_min: 10
---

# s05 · approval：审批策略 + EvExecApprovalRequest 往返

> 教什么：把 s03 的 shell 和 s04 的 apply_patch 套一层"用户允许了再跑"的门。

---

## Problem / 问题

LLM 偶尔会想跑 `rm -rf ~`。即使大多数时候它是对的，"偶尔"也太贵。我们需要一个"先问后做"机制：

- 不是每个用户都同样保守（CI 流水线 vs 本地交互），所以策略可配置；
- 不是每次都问（频繁打断也很烦），所以 trusted 命令直接放行；
- 一旦用户连续拒绝几次，我们应该直接打断这一回合，而不是无限来回。

## Solution / 解决方案

`Gate`：

- 持有 `ApprovalPolicy`（Never / OnFailure / OnRequest / UnlessTrusted / Granular）。
- `Check(ctx, req, emit)` 在"需要批准"时调 `emit(req)`（agent loop 把它转成 `EvExecApprovalRequest`），然后阻塞 channel 等 `Resolve(id, approved)`。
- 一对计数器：连续拒绝 `≥3` 或总共拒绝 `≥10`，置位 `Denied3x`，loop 据此 `EvTurnComplete{Cancelled:true}`。
- ctx 取消等同于 deny。

## How It Works / 工作原理

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

核心 30 行（节选自 [`agents/s05-approval/gate.go`](https://github.com/Ding-Ye/learn-codex/blob/main/agents/s05-approval/gate.go)）：

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

**4 个非显然之处**：

1. **emit 只在确实要问的时候调用**——Never / Trusted-allowed 路径根本不打扰 frontend。
2. **pending 用 map[id]chan**——不同 tool call 并发审批互不干扰；当下一回合的 emit 还没决定时，前一回合可能在等。
3. **ctx 取消视为 deny**——OpInterrupt 或 OpShutdown 期间挂起的审批应当干净撤销。
4. **counter 用 atomic + bool 字段**——counter 自身原子，最终 `Denied3x` 是非原子读 boolean；但只在 `afterDecision` 写、其它地方只读，且我们只在一个 goroutine 里观测它。

## What Changed / 与 s04 的变化

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

s06 会把 `Trusted` 这个函数指针接成 execpolicy DSL 的产物。

## Try It / 动手试一试

```bash
cd agents/s05-approval
go run ./cmd
# 你会看到：
# [approve?] command: [ls /tmp] reason=""
# y/n > y
# decision: Allow ...

go test -v ./...
```

## Upstream Source Reading / 上游源码阅读

```upstream:codex-rs/core/src/codex_thread.rs
// Source: codex-rs/core/src/codex_thread.rs (approval branch — 节选)

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
            AskForApproval::OnFailure => ApprovalDecision::AllowOnce, // ← retry-only path
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

**对照阅读要点**：

- **`OnFailure` 真的存在**：上游 OnFailure 的语义是"先跑，失败了再问要不要重试"。学习版定义了枚举但没实现重试路径——这是一个 readers' exercise。
- **`exec_policy.classify` 返回 Rule**：上游 trusted 检查走的是 execpolicy DSL（s06 一节）。学习版用 `Trusted` 函数指针留口子。
- **`oneshot` channel**：上游用 tokio oneshot 而不是普通 mpsc。Go 里 buffered chan(1) 等价。
- **`record_denial` 触发 `interrupt_turn`**：上游会主动给当前 turn 注入一个 `Op::Interrupt`。学习版只置位 `Denied3x`，由调用者去触发 interrupt。

**想读更多**：从 `codex_thread.rs::run_turn` 下到 `tool_dispatch::shell` 的入口，看 approval gate 怎么 wrap 在 sandbox 之上、execpolicy 之外。这条线是 s05 → s06 → s08 的代码地图。

---

**下一节预告**：s06 把 `Gate.Trusted` 这个函数指针实现成一个真的 DSL：从 `.rules` 文件读、按前缀匹配、决策 `Allow/Prompt/Forbidden`。
