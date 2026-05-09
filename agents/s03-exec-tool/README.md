# s03 · exec tool (streaming shell with cap + timeout)

A faithful Go port of the load-bearing pieces of `codex-rs/core/src/exec.rs`.

- `ExecParams` mirrors the upstream struct (Command, Cwd, Env, Timeout, OutputCapBytes).
- `Run(ctx, p, deltas)` spawns the process, reads stdout/stderr concurrently in 4 KB chunks, sends each chunk on `deltas` (with stream tag + sequence number), enforces a 256 KB output cap (truncates + drains), and respects ctx cancellation / timeout.

## Run

```bash
cd agents/s03-exec-tool
go run ./cmd -- ls -la /tmp
go test -v ./...
```

## Upstream

- [`codex-rs/core/src/exec.rs`](https://github.com/openai/codex/blob/main/codex-rs/core/src/exec.rs) — `process_exec_tool_call`, `consume_output`.
- [`upstream-readings/s03-exec.rs`](../../upstream-readings/s03-exec.rs).
