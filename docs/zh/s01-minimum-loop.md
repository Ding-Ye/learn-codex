---
title: "s01 · 最小 agent loop：Op / EventMsg 协议"
chapter: 1
slug: s01-minimum-loop
est_read_min: 12
---

# s01 · 最小 agent loop：Op / EventMsg 协议

> 教什么：codex 的整个核心其实只是一个 goroutine + 两个 channel。先把协议形状立起来，后面九节都是往这个 loop 上挂东西。

---

## Problem / 问题

如果你打开 `codex-rs/`，第一眼看到的是 120+ 个 crate、Bazel + Cargo 双构建系统、tokio async runtime、Ratatui TUI、Seatbelt/Landlock 沙盒、MCP 协议——这一堆东西很容易让人以为 codex 是一个"庞大的 agent 框架"。

它不是。

把所有外围拨开，codex 的本质是一个 **submission queue → goroutine → event channel** 的小型状态机。前端往里 push 用户输入，后端往外 pop 出"agent 在干嘛"的事件。模型调用、工具执行、审批、沙盒——每一节都只是给这个 loop 多挂一个 case。如果第一节不把 Op / EventMsg 的形状钉死，后面九节就会一直在改协议。

## Solution / 解决方案

我们把上游 `codex-rs/protocol/src/protocol.rs` 里的两个核心枚举搬到 Go 里：

```
Op       (frontend → backend)         EventMsg  (backend → frontend)
─────────────────────────────         ──────────────────────────────
OpUserInput {Text}                    EvTurnStarted {TurnID}
OpExecApproval {ID, Approved}         EvAgentMessage {TurnID, Text}
OpInterrupt                           EvExecApprovalRequest {ID, …}
OpShutdown                            EvTurnComplete {TurnID, Cancelled}
                                      EvError {Message}
                                      EvShutdownComplete
```

三个关键决策：

1. **接口标记 + 哨兵方法**做 tagged union——Go 没有 Rust 风格的 enum，但 `interface{ isOp() }` + 私有 `isOp()` 实现可以让类型 switch 在编译期穷举。封闭性与 Rust enum 等价。
2. **不在 s01 里调用 LLM**——s02 才会接 OpenAI。s01 的 `runTurn` 只是把用户文字加上 `"echo: "` 前缀回写成 `EvAgentMessage`。读者读完这节应该看到"协议管道在跑"，而不是被 streaming SSE 吓退。
3. **submission queue 是 buffered chan**——容量 16，任何时刻可以预先排队多条 op（包括 `OpInterrupt`），不会因为 frontend "投递太快"而死锁。

## How It Works / 工作原理

```
   frontend                       backend (goroutine)
   ────────                       ───────────────────
   c.Submit(OpUserInput)──┐
                          ▼
                   ┌──────────────┐
                   │ subs (chan)  │
                   └──────┬───────┘
                          ▼
                       run()  ──── select ────┬── ctx.Done() → return
                                               │
                                               └── sub := <-subs
                                                       │
                                              ┌────────┴────────┐
                                              ▼                 ▼
                                          OpUserInput        OpShutdown
                                              │                 │
                                              ▼                 ▼
                              EvTurnStarted              EvShutdownComplete
                              EvAgentMessage             close(events)
                              EvTurnComplete
                                              │
                                              ▼
                                    ┌──────────────┐
                                    │ events (chan)│ ──→ frontend ranges
                                    └──────────────┘
```

核心 30 行（节选自 [`agents/s01-minimum-loop/codex.go`](https://github.com/Ding-Ye/learn-codex/blob/main/agents/s01-minimum-loop/codex.go)）：

```go
func (c *Codex) run(ctx context.Context) {
    defer close(c.events)
    for {
        select {
        case <-ctx.Done():
            return
        case sub, ok := <-c.subs:
            if !ok { return }
            if !c.handle(ctx, sub) {
                c.events <- EvShutdownComplete{}
                return
            }
        }
    }
}

func (c *Codex) runTurn(ctx context.Context, turnID, text string) {
    c.events <- EvTurnStarted{TurnID: turnID}
    if c.peekInterrupt() {
        c.events <- EvTurnComplete{TurnID: turnID, Cancelled: true}
        return
    }
    c.events <- EvAgentMessage{TurnID: turnID, Text: "echo: " + text}
    c.events <- EvTurnComplete{TurnID: turnID, Cancelled: false}
}
```

**4 个非显然之处**：

1. **events channel 在 goroutine 里 `defer close`**——前端用 `for ev := range c.Events()` 即可干净退出；不需要额外的 done 信号。
2. **interrupt 是 cooperative 的**——s01 里每个 turn 之间检查一次 `peekInterrupt()`。s05 加 per-turn ctx 之后会变成"当前 LLM 流式输出可中断"，但协议不变。
3. **`OpExecApproval` 现在就定义了**——虽然 s05 才会有人发它。这样后续节就不需要回头改 op.go。这是上游 codex 的设计哲学：**协议先定，行为后加**。
4. **submission id 是用 `atomic.Uint64` 自增的**——多 goroutine 并发 Submit 也能拿到不同 id，正好对得上 `Submission.ID` → `EventMsg.TurnID` 的回指关系。

## What Changed / 与 s00 的变化

s01 是第一节，没有上一节。

如果非要对比，是相对于"完全没有 codex 项目"的状态：你现在拥有一个能跑、有 4 个测试覆盖的 echo agent，和一份接下来九节都要扩展的协议。

## Try It / 动手试一试

```bash
cd agents/s01-minimum-loop

# REPL 模式
go run ./cmd
# 然后输入：
#   > hello world
#   > /interrupt    （没有 turn 在跑时是 no-op）
#   > /quit

# 跑测试
go test -v ./...
```

期望输出形态：

```
=== RUN   TestSubmitProducesTurnStartedAndComplete
--- PASS: TestSubmitProducesTurnStartedAndComplete (0.00s)
=== RUN   TestInterruptStopsTurnEarly
--- PASS: TestInterruptStopsTurnEarly (0.00s)
=== RUN   TestShutdownClosesEventChannel
--- PASS: TestShutdownClosesEventChannel (0.00s)
=== RUN   TestSubmitReturnsUniqueIDs
--- PASS: TestSubmitReturnsUniqueIDs (0.00s)
PASS
```

## Upstream Source Reading / 上游源码阅读

上游对应 `codex-rs/protocol/src/protocol.rs`（枚举定义）和 `codex-rs/core/src/codex_thread.rs`（`CodexThread::submit` / `next_event`）。完整版有 ~30 个 EventMsg 变种、~20 个 Op 变种、tracing 上下文、原子 submission id；我们这一节只取最 load-bearing 的 4+6 个。

```upstream:codex-rs/protocol/src/protocol.rs#L1-L80
// Source: codex-rs/protocol/src/protocol.rs (节选)
//
// 上游用 #[derive(Serialize, Deserialize)] 让协议可上 wire；我们这一节
// 不需要 wire 形式，所以省掉。
pub enum Op {
    UserInput { text: String },
    ExecApproval { id: String, approved: bool },
    PatchApproval { id: String, approved: bool },
    Interrupt,
    Shutdown,
    ReloadUserConfig,
    // …还有 ~14 个变种：编辑 prompt、切换 profile、加 MCP server 等
}

pub enum EventMsg {
    TurnStarted { turn_id: String },
    AgentMessage { turn_id: String, text: String },
    ExecCommandBegin { id: String, command: Vec<String>, cwd: PathBuf },
    ExecCommandEnd   { id: String, exit_code: i32 },
    ExecOutputDelta  { id: String, stream: Stream, bytes: Vec<u8> },
    ExecApprovalRequest { id: String, command: Vec<String>, reason: String },
    PatchApplyBegin  { id: String, path: PathBuf },
    PatchApplyEnd    { id: String, success: bool },
    PatchApprovalRequest { id: String, path: PathBuf, patch_text: String },
    ContextCompacted { turn_id: String, summary: String },
    Error { message: String },
    TurnComplete { turn_id: String },
    ShutdownComplete,
    // …还有 ~17 个变种
}

pub struct Submission {
    pub id: SubmissionId,
    pub op: Op,
    pub trace_context: Option<TracingContext>,  // W3C trace context
}
```

**对照阅读要点**：

- **协议封闭性**：上游 Rust enum 是真正的封闭代数类型。我们用 `interface { isOp() }` + 私有方法模拟——任何外部包没法实现 `isOp()`，所以集合是封闭的。等价但要靠约定。
- **TracingContext**：上游每个 Submission 都带 W3C trace context，方便接 OTel。学习版省掉，但要意识到生产级 agent 这是必需的。
- **ExecCommandBegin / End / OutputDelta** 这一组：上游把 shell 工具的"开始/输出/结束"完整地建模在协议层。我们 s03 做 exec tool 时会回到这里。
- **ContextCompacted / ReloadUserConfig**：上游有上下文压缩和热重载。学习版不做，但是协议留口让你将来加。
- **submit / next_event 是 async fn**：上游用 tokio。我们用 goroutine + channel——形状一样、心智模型一样、Rust 异步运行时的负担没有了。

**想读更多**：从 `codex-rs/core/src/codex_thread.rs` 的 `submit()` 入手，跟着 `Submission` 进 `codex_delegate.rs` 的 dispatch 表，最后看 `codex.rs` 里 `next_event_inner()` 怎么把内部状态机的 transition 翻译成 EventMsg。这条线就是 s01 → s02 → s05 的真实代码地图。

---

**下一节预告**：s02 把"echo"换成真正的 OpenAI Chat Completions 流式调用，并把模型返回的 tool_call 投递成 `EvAgentMessage` + 一个挂钩点，给 s03 的 shell tool 留好接口。
