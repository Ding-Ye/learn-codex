---
title: "s01 · Minimum loop: Op / EventMsg protocol"
chapter: 1
slug: s01-minimum-loop
est_read_min: 12
---

# s01 · Minimum loop: Op / EventMsg protocol

> What this teaches: codex's entire core is, at heart, one goroutine + two channels. We pin down the protocol shape first; the next nine sessions just hang behaviour off this loop.

---

## Problem

When you first open `codex-rs/`, you see 120+ Rust crates, a Bazel + Cargo dual build, tokio everywhere, a Ratatui TUI, Seatbelt/Landlock sandboxes, MCP servers — it's tempting to assume codex is "a sprawling agent framework".

It isn't.

Strip away the periphery and codex is a **submission queue → goroutine → event channel** state machine. The frontend pushes user input in, the backend pops "what the agent is doing" events out. Model calls, tool execution, approval, sandboxing — every later session just adds another `case` to this loop. If we don't lock down the shape of `Op` and `EventMsg` in s01, we'll be re-shaping the protocol for nine more chapters.

## Solution

We port the two core enums from upstream `codex-rs/protocol/src/protocol.rs` into Go:

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

Three key decisions:

1. **Marker-method tagged unions.** Go doesn't have Rust enums, but `interface{ isOp() }` + a private `isOp()` implementation gives us closed sets enforceable at compile time via type switches. Sealing equivalent to Rust enums.
2. **No LLM call in s01.** That's s02. The s01 `runTurn` just prefixes the user text with `"echo: "` and sends it back as `EvAgentMessage`. The reader should see "the protocol pipe runs" before getting buried in streaming SSE.
3. **Submission queue is a buffered chan (size 16).** The frontend can pre-queue ops (including `OpInterrupt`) without deadlocking when it submits faster than the backend drains.

## How It Works

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

The core 30 lines (excerpted from [`agents/s01-minimum-loop/codex.go`](https://github.com/Ding-Ye/learn-codex/blob/main/agents/s01-minimum-loop/codex.go)):

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

**4 non-obvious points**:

1. **`defer close(c.events)`** — the frontend just `for ev := range c.Events()`s out to clean exit. No separate done signal.
2. **Interrupt is cooperative.** s01 checks `peekInterrupt()` between turns. s05 will add a per-turn ctx so "the in-flight model stream can be cancelled", but the protocol stays the same.
3. **`OpExecApproval` is defined now**, even though nothing emits it until s05. Later sessions don't have to retroactively edit `op.go`. That's upstream codex's design philosophy: **protocol first, behaviour later**.
4. **Submission ids come from `atomic.Uint64`** — concurrent `Submit` calls get distinct ids. They line up with `EventMsg.TurnID` so the frontend can correlate "request → its events".

## What Changed (vs. s00)

s01 is the first chapter; there is no s00.

The closest comparison is to "no codex project at all": you now own a runnable, four-test-covered echo agent, plus a protocol that the next nine sessions extend.

## Try It

```bash
cd agents/s01-minimum-loop

# REPL mode
go run ./cmd
# Then type:
#   > hello world
#   > /interrupt    (no-op when no turn is in flight)
#   > /quit

# Tests
go test -v ./...
```

Expected output shape:

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

## Upstream Source Reading

The upstream equivalent lives in `codex-rs/protocol/src/protocol.rs` (enum definitions) and `codex-rs/core/src/codex_thread.rs` (`CodexThread::submit` / `next_event`). The full version has ~30 `EventMsg` variants, ~20 `Op` variants, tracing context, atomic submission ids; we keep just the load-bearing 4+6.

```upstream:codex-rs/protocol/src/protocol.rs#L1-L80
// Source: codex-rs/protocol/src/protocol.rs (excerpt)
//
// Upstream uses #[derive(Serialize, Deserialize)] so the protocol can travel
// over a wire (stdio JSON-RPC, app-server WebSocket). We don't need that in
// s01 so the trait derives are stripped.
pub enum Op {
    UserInput { text: String },
    ExecApproval { id: String, approved: bool },
    PatchApproval { id: String, approved: bool },
    Interrupt,
    Shutdown,
    ReloadUserConfig,
    // ~14 more variants: edit prompt, switch profile, add MCP server, etc.
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
    // ~17 more variants
}

pub struct Submission {
    pub id: SubmissionId,
    pub op: Op,
    pub trace_context: Option<TracingContext>,  // W3C trace context
}
```

**Reading notes**:

- **Protocol sealing.** Upstream Rust enums are genuinely closed algebraic types. We approximate with `interface{ isOp() }` + a private method — packages outside this one can't implement `isOp()`, so the set is closed by convention.
- **TracingContext.** Upstream attaches W3C trace context to every Submission for OpenTelemetry integration. The teaching version skips it, but recognise that production agents need it.
- **`ExecCommandBegin` / `End` / `OutputDelta`** — upstream models the shell tool's "start / streaming output / finish" entirely in the protocol layer. We'll come back to this in s03 when we build the exec tool.
- **`ContextCompacted` / `ReloadUserConfig`** — upstream supports context compaction and hot config reload. Skipped here, but the protocol keeps room.
- **`submit` / `next_event` are `async fn`.** Upstream runs on tokio. We use a goroutine + channels — same shape, same mental model, no async-runtime baggage.

**Read further**: start at `codex-rs/core/src/codex_thread.rs::submit`, follow `Submission` into `codex_delegate.rs`'s dispatch table, then read `codex.rs::next_event_inner` to see how the internal state-machine transitions become EventMsgs. That trace is the real-source map for s01 → s02 → s05.

---

**Next**: s02 swaps "echo" for real OpenAI Chat Completions streaming, and exposes a hook for s03 to dispatch any tool_call the model emits.
