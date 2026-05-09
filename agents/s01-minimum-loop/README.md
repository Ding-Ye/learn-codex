# s01 · minimum loop

The smallest agent that earns the name. **No model call yet.** Just the protocol shape:

- `Op` (frontend → backend): `OpUserInput`, `OpInterrupt`, `OpShutdown`, `OpExecApproval`
- `EventMsg` (backend → frontend): `EvTurnStarted`, `EvAgentMessage`, `EvTurnComplete`, `EvShutdownComplete`, `EvExecApprovalRequest`, `EvError`
- A goroutine drains submissions, emits events, "echoes" the user input as the assistant message.

## Run

```bash
cd agents/s01-minimum-loop
go run ./cmd
```

```
learn-codex s01 minimum loop. Type a line; '/quit' to exit.
> hello
  [turn sub-1 started]
  agent: echo: hello
  [turn sub-1 done]
> /quit
  [shutdown]
```

## Test

```bash
go test -v ./...
```

## Why it matters

Codex's upstream `CodexThread::submit / next_event` pair is the hinge the entire agent rotates on. Every later session — model client, exec, approval, rollout, MCP — attaches new behaviour to *this same loop*. Get the protocol right and everything else is straight-line code.

## Upstream

- [`codex-rs/protocol/src/protocol.rs`](https://github.com/openai/codex/blob/main/codex-rs/protocol/src/protocol.rs) — the real `EventMsg` and `Op` enums (~30 + ~20 variants).
- [`codex-rs/core/src/codex_thread.rs`](https://github.com/openai/codex/blob/main/codex-rs/core/src/codex_thread.rs) — `CodexThread::submit`, `CodexThread::next_event`.
- See [`upstream-readings/s01-protocol.rs`](../../upstream-readings/s01-protocol.rs) for an annotated excerpt.
