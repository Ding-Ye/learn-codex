---
title: "s03 · exec 工具：流式输出 + 截断 + 超时"
chapter: 3
slug: s03-exec-tool
est_read_min: 12
---

# s03 · exec 工具：流式输出 + 截断 + 超时

> 教什么：把 s02 暴露的 tool call（shell 命令）真正跑起来。三件大事：实时流出 stdout/stderr、按字节硬截断、超时即杀。

---

## Problem / 问题

s02 让模型说出 `tool_call: shell({"cmd":"go test ./..."})`，但我们没接住。要让 agent 真的"做事"，shell 工具是第一个、也是最常用的。

实现时三个真痛点：

1. **不能等命令跑完才返回**——`go test` 跑 20 秒，前端要边跑边看输出。
2. **不能让命令吃光内存**——LLM 一旦发出 `find / -type f`，stdout 可能是几 GB。我们硬截断 256 KB，超过的字节静默丢弃但**继续 drain pipe**，否则 child process 因 SIGPIPE 死掉，exit code 全错。
3. **不能挂死**——`sleep 9999` 必须能在 `Timeout` 到期时被杀干净。

## Solution / 解决方案

`Run(ctx, ExecParams, deltas chan<- OutputDelta) ExecResult`：

- 起 child 用 `exec.CommandContext`——ctx 取消即 SIGKILL。
- 起两个 goroutine 分别读 stdout/stderr，每读到一个 chunk 就：
  - 写进 `bytes.Buffer`（用于最终 `ExecResult.Stdout/Stderr`）
  - 通过 `deltas` channel 实时发出（带 `Stream`/`Seq` 标记）
- 一个 mutex 守 `written int` 计数器：超过 cap 就把这条 chunk 截短、`Truncated = true`、然后 `io.Copy(io.Discard, r)` 把 pipe 剩余的字节耗光，让 child 能正常 exit。
- `Timeout > 0` 时把 ctx 包成 `WithTimeout`。

## How It Works / 工作原理

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

核心 25 行（节选自 [`agents/s03-exec-tool/exec.go`](https://github.com/Ding-Ye/learn-codex/blob/main/agents/s03-exec-tool/exec.go)）：

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

**4 个非显然之处**：

1. **drain after truncate**——超过 cap 之后必须 `io.Copy(io.Discard, r)` 把 pipe 剩下的字节读完。否则 child 写 stdout 阻塞→ child 不退出 → `cmd.Wait()` 不返回，整个 agent 死锁。
2. **共享 `written` 计数**——stdout 和 stderr 算一个 cap，所以两个 pump 共享 mutex。要不然小的 stdout + 大的 stderr 会绕过 cap。
3. **chunk = 4 KB**——上游也是这个数（`exec.rs` 的 `BUF_SIZE`）。够小让 streaming 看起来流畅，够大避免太多 syscall。
4. **`exec.CommandContext` 自动杀**——ctx cancel 时 Go 标准库会发 SIGKILL。这就是 timeout 实现的全部魔法。

## What Changed / 与 s02 的变化

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

s02 没接 tool 执行；s03 把 tool 的"shell 这一种"做完了。s05 会把 `EvToolCallRequested` → `Run()` 的桥接接通过审批门。

## Try It / 动手试一试

```bash
cd agents/s03-exec-tool

# Streaming demo
go run ./cmd -- sh -c 'for i in 1 2 3; do echo $i; sleep 0.5; done'
# 你会看到 1、2、3 一行一行实时出现，而不是一次性

# Cap demo
go run ./cmd -- sh -c 'yes A | head -c 1000000'    # 1 MB → 截到 256 KB

go test -v ./...
```

## Upstream Source Reading / 上游源码阅读

```upstream:codex-rs/core/src/exec.rs
// Source: codex-rs/core/src/exec.rs (节选)
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
    // 3. spawn two tasks: each reads up to BUF_SIZE bytes, sends as
    //    ExecCommandOutputDelta on delta_tx, enforces shared output cap.
    // 4. on cap overflow: send marker + drain remaining
    // 5. on timeout: drop CancellationToken → child gets SIGKILL via tokio
    // 6. await child.wait() → exit_code
    // 7. detect sandbox-denial via stderr keyword sniffing
}
```

**对照阅读要点**：

- **共享 output cap**：上游也是 stdout+stderr 共用一个 cap。理由相同——别让 stderr 大溢出绕开 stdout 的限制。
- **`network_proxy` 字段**：上游能给 child 注入 HTTP_PROXY 环境，方便沙盒里的命令通过 codex 控制的代理出网。学习版省掉。
- **`sandbox_permission_level` 字段**：在 s08 沙盒一节回头加。
- **`ExecExpiration` 枚举**：Timeout 和 Cancellation 二选一。Go 里用 ctx 可以同时表达，更省。
- **sandbox-denial 关键字嗅探**：上游会扫 stderr 里有没有 "Operation not permitted" 之类，用来给用户提示"你触发沙盒了"。学习版省掉，但 sandbox 章节会回到这个细节。

**想读更多**：从 `exec.rs::process_exec_tool_call` 的 BufReader 循环入手，跟着 `ExecCommandOutputDelta` 的 channel 进 `core/src/codex_thread.rs` 的 `record_exec_output`，看 delta 怎么 fan-out 给 TUI 和 rollout 两个 consumer。

---

**下一节预告**：s04 不动 shell，给 agent 加第二个工具——`apply_patch`，专门处理 LLM 编辑文件的请求。
