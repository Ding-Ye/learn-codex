---
title: "s07 · rollout：JSONL 会话日志 + resume"
chapter: 7
slug: s07-rollout
est_read_min: 10
---

# s07 · rollout：JSONL 会话日志 + resume

> 教什么：怎么让 agent 经历 crash / 关机 / `Ctrl+C` 之后还能在下次启动时**继续上一次对话**。append-only JSONL + 一个 replayer，就这两件事。

---

## Problem / 问题

`go test ./pkg/foo/...` 跑了 5 分钟，模型读了一堆文件、写了几个 patch，正打算 verify 的时候——网线断了，进程被杀了，电池没了。如果 codex 把整个会话状态留在内存里，等于让用户从头重写需求。

需求很简单：

1. 每条 `OpUserInput` / 模型响应 / 工具调用 / 工具结果——立刻落盘。
2. 落盘格式必须**人能 cat**——这是 codex 选 JSONL 而非 sqlite 的核心理由。
3. 进程崩在写一半（电源断了/SIGKILL）的最后一行——下次能 cat 出前面的好行，自动跳过烂尾。
4. 多个并发写（emit goroutine + recorder goroutine）不会互相蹩。

## Solution / 解决方案

`Recorder`：

- 一个 `os.OpenFile(path, O_APPEND|O_CREATE|O_WRONLY, 0o644)` 文件句柄。
- 一个 `sync.Mutex` 串行化所有 `Record(kind, payload)` 调用——`Write()` 在 O_APPEND 模式下原子 ≤ PIPE_BUF (4 KB)，但我们的行有可能更大，所以用 mutex 保险。
- `Record` 把 `RolloutItem{Kind, TS, Payload}` 序列化成一行 JSON + `\n` 后 append。

`Replayer`：

- `bufio.Scanner` 按行读；每一行 try `json.Unmarshal(&RolloutItem)`。
- 失败的行（截断的最后一行）打印一条 stderr warning 然后跳过——不让一行烂尾毁掉整个会话。
- `Reconstruct(items)` 提取 user/assistant/tool 三类，打平成 `[]ResumedMessage`，agent loop 可以直接 splice 进 `provider.Stream` 的 `messages` 数组。

## How It Works / 工作原理

```
Codex                                   ~/.codex-learn/sessions/<id>.jsonl
─────                                   ──────────────────────────────────
loop emits EvAgentMessage          ─▶  Record("assistant_text", {"text":"…"})
                                       │
                                       │ json.Marshal(RolloutItem{kind,ts,payload})
                                       │ mu.Lock(); f.Write(line+"\n"); mu.Unlock()
                                       ▼
                                       {"kind":"session_meta","ts":1715000000000,"payload":{"id":"sess-1",...}}
                                       {"kind":"user_input","ts":1715000005000,"payload":{"text":"hi"}}
                                       {"kind":"assistant_text","ts":1715000007000,"payload":{"text":"hello"}}
                                       {"kind":"assistant_tool_call","ts":...,"payload":{"id":"call_1","name":"shell","args":"…"}}
                                       {"kind":"tool_result","ts":...,"payload":{"id":"call_1","output":"…"}}
                                       
on resume:                             ▲
─────────                              │
Replay(path) ─▶ (meta, items)  ◀───────┘
Reconstruct(items) ─▶ []ResumedMessage
                          ▼
loop.history = splice(meta.system + ResumedMessages...)
provider.Stream(...)  // continues from where we left off
```

核心 25 行（节选自 [`agents/s07-rollout/recorder.go`](https://github.com/Ding-Ye/learn-codex/blob/main/agents/s07-rollout/recorder.go) 和 `replayer.go`）：

```go
func (r *Recorder) Record(kind string, payload any) error {
    b, err := json.Marshal(payload)
    if err != nil { return err }
    item := RolloutItem{Kind: kind, TS: time.Now().UnixMilli(), Payload: b}
    line, _ := json.Marshal(item)
    r.mu.Lock(); defer r.mu.Unlock()
    _, err = r.f.Write(append(line, '\n'))
    return err
}

func Replay(path string) (*SessionMeta, []RolloutItem, error) {
    f, _ := os.Open(path); defer f.Close()
    scanner := bufio.NewScanner(f); scanner.Buffer(make([]byte, 1<<20), 1<<20)
    var meta *SessionMeta; var items []RolloutItem
    for scanner.Scan() {
        var item RolloutItem
        if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
            fmt.Fprintf(os.Stderr, "rollout: skipping bad line: %v\n", err)
            continue   // ← truncation tolerance
        }
        if item.Kind == KindSessionMeta && meta == nil { /* extract */ continue }
        items = append(items, item)
    }
    return meta, items, nil
}
```

**4 个非显然之处**：

1. **resume 时不重写 meta**——`New(path, meta)` 看到文件非空就跳过 meta 写。这样 `meta.id` 永远是首次会话的 id，便于跨重启关联。
2. **truncation tolerance**——torn last line 打 warning 跳过，不报致命错。这是上游 `rollout/src/lib.rs::recover_from_partial_write` 的简化版。
3. **`O_APPEND` + mutex** 不是冗余：O_APPEND 保证单次 `write(2)` 系统调用原子，但**多次** `write` 之间可能交错；mutex 把"一行 = 一次 write"封死。
4. **不 fsync**——上游也通常不 fsync 每条记录，性能太差。学习版同样省略。代价是断电那一刻可能丢最近 1-2 行；这是 codex 接受的 trade。

## What Changed / 与 s06 的变化

```diff
+ type RolloutItem struct { Kind string; TS int64; Payload json.RawMessage }
+ type SessionMeta struct { ID, Model, Cwd string; StartedAt int64; Extras map[string]string }
+ // …per-kind payload structs (UserInputItem, AssistantTextItem, AssistantToolCallItem, ToolResultItem, EventItem)
+
+ type Recorder struct { /* path, mu, f */ }
+ func New(path, meta) (*Recorder, error)
+ func (r *Recorder) Record(kind string, payload any) error
+
+ func Replay(path) (*SessionMeta, []RolloutItem, error)
+ func Reconstruct(items) []ResumedMessage
```

s05/s06 不需要 import s07——但 s10 集成时会把 Recorder 接入 codex.events 主路径。

## Try It / 动手试一试

```bash
cd agents/s07-rollout

# 写一个 demo 会话
go run ./cmd -mode=record -path=/tmp/learn-codex.jsonl
cat /tmp/learn-codex.jsonl
# 你能看到三行 JSON：session_meta / user_input / assistant_text

# 读回来
go run ./cmd -mode=replay -path=/tmp/learn-codex.jsonl

go test -v ./...
```

## Upstream Source Reading / 上游源码阅读

```upstream:codex-rs/rollout/src/recorder.rs
// Source: codex-rs/rollout/src/recorder.rs (节选)

pub struct RolloutRecorder {
    tx: Sender<RolloutCmd>,                    // ★ async channel; main thread doesn't block on disk
    writer_task: Arc<RolloutWriterTask>,
    rollout_path: PathBuf,
    event_persistence_mode: EventPersistenceMode,  // SaveAll | None
}

pub enum RolloutItem {
    SessionMetadata(SessionMetadata),
    ResponseItem { from_responses_api: ResponseItem },
    CompactedItems { summary: String, dropped_count: usize },
    ContextUpdate { kind: ContextKind, content: String },
    EventMessage { event: EventMsg },
}

impl RolloutRecorder {
    pub async fn record_items(&self, items: Vec<RolloutItem>) {
        // Non-blocking send to writer task; if the channel is full, drop
        // oldest item with a warning. The writer task fsync's periodically
        // (not every item) for performance.
        let _ = self.tx.send(RolloutCmd::Items(items)).await;
    }
}

pub fn list_sessions(home: &Path) -> Vec<RolloutMetadata> {
    // walks `~/.codex/sessions/*.jsonl`, parses just the meta line of each,
    // returns sorted by started_at DESC for the resume picker UI
}
```

**对照阅读要点**：

- **async channel + writer task**：上游用一个独立 task 写盘，主 thread 通过 channel 投递。学习版直接 mutex+write——代码简单但 main thread 的"写盘"是同步的。生产里你会想要 async pattern。
- **`CompactedItems` 变种**：上游会主动 compact（把旧的工具调用结果摘要成一句话）以省 token。学习版省。
- **`EventPersistenceMode`**：决定记不记 EventMsg。SaveAll 用于 debug 重放，None 用于普通用户。学习版默认 SaveAll。
- **`list_sessions`**：上游有 resume picker UI（codex resume 时弹列表）。学习版 s10 会做最简版。
- **periodic fsync**：上游不每条 fsync，但**周期性**（每秒或每 N 条）会 fsync 一次。学习版完全不 fsync。

**想读更多**：从 `recorder.rs::record_items` 入手，跟着 `writer_task` 看 `RolloutWriterTask::run` 怎么 batch 写、怎么处理 channel 满；再看 `core/src/codex.rs::handle_event` 调用 `record_items` 的几个 callsite——这条线是 s07 → s10 的代码地图。

---

**下一节预告**：s08 给 s03 的 shell 工具加上真的 OS 沙盒——macOS Seatbelt 拦内核、Linux Landlock 拦 syscall。
