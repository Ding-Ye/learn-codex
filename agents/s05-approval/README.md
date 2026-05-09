# s05 · approval gate

Before s03's shell tool or s04's apply_patch tool runs, give the user a chance to say no.

- `ApprovalPolicy` enum: Never / OnFailure / OnRequest / UnlessTrusted / Granular.
- `Gate.Check(ctx, req, emit)` — non-blocking under `Never`; under others, calls `emit(req)` (the agent loop converts that into `EvExecApprovalRequest`) and blocks until `Gate.Resolve(id, approved)` is called.
- Counters: 3 consecutive or 10 total denials → `g.Denied3x = true` (turn should interrupt).

## Run

```bash
cd agents/s05-approval
go run ./cmd        # interactive y/n demo
go test -v ./...
```

## Upstream

- [`codex-rs/core/src/codex_thread.rs`](https://github.com/openai/codex/blob/main/codex-rs/core/src/codex_thread.rs) — approval branch + denial counter.
- [`codex-rs/protocol/src/protocol.rs`](https://github.com/openai/codex/blob/main/codex-rs/protocol/src/protocol.rs) — `AskForApproval` enum.
- [`upstream-readings/s05-approval.rs`](../../upstream-readings/s05-approval.rs).
