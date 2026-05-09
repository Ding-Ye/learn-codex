---
title: "s03 · exec tool: streaming + cap + timeout"
chapter: 3
slug: s03-exec-tool
est_read_min: 12
---

# s03 · exec tool: streaming + cap + timeout

> What this teaches: actually run the shell tool calls s02 surfaced. Three big concerns: real-time stdout/stderr streaming, hard byte-cap, and clean timeout-kill.

---

## Problem

s02 lets the model emit `tool_call: shell({"cmd":"go test ./..."})`, but we never executed it. For an agent to actually *do* things, the shell tool is the first one to build — and the most-used one.

Three real pains in the implementation:

1. **Can't wait for completion.** A 20-second `go test` should stream output as it runs.
2. **Can't let the command exhaust memory.** If the LLM emits `find / -type f`, stdout could be gigabytes. We hard-truncate at 256 KB but **must keep draining the pipe**, otherwise the child blocks on writes and the exit code is lost.
3. **Can't hang.** A `sleep 9999` must be killable when `Timeout` expires.

## Solution

`Run(ctx, ExecParams, deltas chan<- OutputDelta) ExecResult`:

- Spawn the child via `exec.CommandContext` — ctx cancel sends SIGKILL automatically.
- Two pump goroutines for stdout/stderr; each reads in 4 KB chunks, writes into a buffered byte slice, and sends an `OutputDelta` on `deltas`.
- A shared `written` counter under one mutex enforces the cap. On overflow: trim the chunk, set `Truncated=true`, then `io.Copy(io.Discard, r)` to drain the remaining bytes so the child can exit cleanly.
- `Timeout > 0` wraps ctx in `WithTimeout`.

## How It Works

```
Run(ctx, ExecParams{Command:[]string{"sh","-c","go test ./..."}, Timeout: 30s, OutputCapBytes: 256<<10})
       │
       │ exec.CommandContext (auto-kills on ctx cancel)
       ▼
  ┌──────────────┐    Pipe(stdout)  ┌──────────────────────┐  deltas chan
  │              │ ────────────────▶│ pump goroutine #1    │ ────────────▶ OutputDelta{StreamStdout, bytes, seq}
  │   /bin/sh    │                  │   - 4 KB chunks      │
  │              │    Pipe(stderr)  │   - mutex-guarded    │
  │              │ ────────────────▶│   - cap check        │ ────────────▶ OutputDelta{StreamStderr, ...}
  └──────────────┘                  │   - drain on overflow│
         │                          └──────────────────────┘
         │                          ┌──────────────────────┐
         └─────────────────────────▶│ pump goroutine #2    │ (stderr)
                                    └──────────────────────┘
                                              ▼
                                          wg.Wait()
                                              ▼
                                          cmd.Wait() → exit code
                                              ▼
                                  ExecResult{ExitCode, Stdout, Stderr, Truncated, Err}
```

Core 25 lines from [`agents/s03-exec-tool/exec.go`](https://github.com/Ding-Ye/learn-codex/blob/main/agents/s03-exec-tool/exec.go):

```go
pump := func(s Stream, r io.Reader, buf *bytes.Buffer) {
    defer wg.Done()
    chunk := make([]byte, 4096)
    for {
        n, err := r.Read(chunk)
        if n > 0 {
            mu.Lock()
            if written+n > p.OutputCapBytes {
                take := p.OutputCapBytes - written
                if take > 0 {
                    buf.Write(chunk[:take]); written += take
                    deltas <- OutputDelta{Stream: s, Bytes: chunk[:take], Seq: seq + 1}
                }
                truncated = true
                mu.Unlock()
                _, _ = io.Copy(io.Discard, r)   // drain so child can exit
                return
            }
            buf.Write(chunk[:n]); written += n
            deltas <- OutputDelta{Stream: s, Bytes: chunk[:n], Seq: seq + 1}
            mu.Unlock()
        }
        if err != nil { return }
    }
}
```

**4 non-obvious points**:

1. **Drain after truncate.** Once cap is hit, you *must* `io.Copy(io.Discard, r)` the rest. Otherwise the child blocks on stdout writes → never exits → `cmd.Wait()` hangs → entire agent deadlocks.
2. **Shared `written` counter.** stdout+stderr share one cap, so both pumps share the mutex. Otherwise a small stdout + large stderr would bypass the limit.
3. **Chunk = 4 KB.** Upstream uses the same number (`exec.rs::BUF_SIZE`). Small enough for smooth streaming, big enough to avoid syscall storm.
4. **`exec.CommandContext` auto-kills.** Go's stdlib sends SIGKILL when ctx cancels. That's the entire timeout magic.

## What Changed (vs. s02)

```diff
+ type ExecParams struct {
+     Command []string; Cwd string; Env map[string]string
+     Timeout time.Duration; OutputCapBytes int
+ }
+ type OutputDelta struct { Stream Stream; Bytes []byte; Seq int }
+ type ExecResult struct { ExitCode int; Stdout, Stderr []byte; Truncated bool; Err error }
+
+ func Run(ctx context.Context, p ExecParams, deltas chan<- OutputDelta) ExecResult { /* … */ }
```

s02 didn't connect tool execution; s03 finishes the shell variant. s05 will gate `EvToolCallRequested → Run()` through the approval policy.

## Try It

```bash
cd agents/s03-exec-tool

# Streaming demo
go run ./cmd -- sh -c 'for i in 1 2 3; do echo $i; sleep 0.5; done'
# you'll see 1, 2, 3 appear one-at-a-time, not all at once

# Cap demo
go run ./cmd -- sh -c 'yes A | head -c 1000000'   # 1 MB → truncated at 256 KB

go test -v ./...
```

## Upstream Source Reading

```upstream:codex-rs/core/src/exec.rs
// Source: codex-rs/core/src/exec.rs (excerpt)
pub struct ExecParams {
    pub command: Vec<String>,
    pub cwd: PathBuf,
    pub env_vars: HashMap<String, String>,
    pub network_proxy: Option<ProxyConfig>,
    pub sandbox_permission_level: SandboxPermissionLevel,
    pub expiration: ExecExpiration,     // Timeout | Cancellation
    pub output_cap_bytes: usize,
}

pub enum ExecExpiration {
    Timeout { millis: u64 },
    Cancellation { token: CancellationToken },
}

pub async fn process_exec_tool_call(
    params: ExecParams,
    delta_tx: Option<mpsc::Sender<ExecCommandOutputDelta>>,
) -> Result<ExecResult, ExecError> {
    // 1. spawn via tokio::process::Command
    // 2. wrap stdout/stderr in BufReader
    // 3. two tasks: each reads up to BUF_SIZE bytes, sends as
    //    ExecCommandOutputDelta on delta_tx, enforces shared output cap.
    // 4. on cap overflow: send marker + drain
    // 5. on timeout: drop CancellationToken → child gets SIGKILL via tokio
    // 6. await child.wait() → exit_code
    // 7. detect sandbox-denial via stderr keyword sniffing
}
```

**Reading notes**:

- **Shared output cap.** Upstream also shares a cap across stdout+stderr. Same reason — prevent stderr blowout from bypassing the stdout limit.
- **`network_proxy` field.** Upstream can inject HTTP_PROXY into the child env so sandboxed commands route through a codex-controlled proxy. Skipped here.
- **`sandbox_permission_level` field.** We add this back in s08 (sandbox).
- **`ExecExpiration` enum.** Upstream's two-variant Timeout|Cancellation can be expressed in Go as a single `context.Context` — cheaper.
- **Sandbox-denial keyword sniffing.** Upstream scans stderr for "Operation not permitted" etc. and surfaces "you tripped the sandbox" hints. Skipped, but s08 returns to the idea.

**Read further**: start at `exec.rs::process_exec_tool_call`'s BufReader loop, follow `ExecCommandOutputDelta` channel into `core/src/codex_thread.rs::record_exec_output`, see how deltas fan out to both the TUI and the rollout consumer.

---

**Next**: s04 leaves shell alone and adds the agent's second tool — `apply_patch`, specifically for LLM file-edit requests.
