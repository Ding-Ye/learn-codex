---
title: "s07 · rollout: JSONL session log + resume"
chapter: 7
slug: s07-rollout
est_read_min: 10
---

# s07 · rollout: JSONL session log + resume

> What this teaches: how the agent survives a crash / shutdown / `Ctrl+C` and **picks up the conversation** on next launch. Append-only JSONL + a replayer — that's it.

---

## Problem

`go test ./pkg/foo/...` runs for 5 minutes. The model has read several files, written a couple of patches, and is just about to verify when — your network drops, the process gets killed, your laptop dies. If codex held all session state in memory, the user has to start over.

The need is simple:

1. Every `OpUserInput` / model response / tool call / tool result — **persist it now**.
2. Persisted format must be **`cat`-able** — that's why codex chose JSONL over sqlite.
3. If the process crashes mid-write (power loss / SIGKILL), the next read should `cat` the good lines and skip the torn last line silently.
4. Multiple concurrent writers (emitter goroutine, recorder goroutine) can't trample each other.

## Solution

`Recorder`:

- One `os.OpenFile(path, O_APPEND|O_CREATE|O_WRONLY, 0o644)` handle.
- A `sync.Mutex` serialises every `Record(kind, payload)` call. (`O_APPEND` makes a single `write(2)` atomic up to PIPE_BUF (4 KB), but our lines can be larger; the mutex guarantees "one line = one write".)
- `Record` marshals `RolloutItem{Kind, TS, Payload}` and appends `line + "\n"`.

`Replayer`:

- `bufio.Scanner` reads line by line; each line `json.Unmarshal`'d to `RolloutItem`.
- Failed lines (the torn last line) get a stderr warning and are skipped — one bad ending doesn't destroy the entire session.
- `Reconstruct(items)` extracts the user/assistant/tool messages in order, returning `[]ResumedMessage` ready to splice into the `messages` array of the next `provider.Stream` call.

## How It Works

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

Core 25 lines from [`agents/s07-rollout/recorder.go`](https://github.com/Ding-Ye/learn-codex/blob/main/agents/s07-rollout/recorder.go) and `replayer.go`:

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

**4 non-obvious points**:

1. **Resume doesn't rewrite meta.** `New(path, meta)` skips the meta write when the file is non-empty. So `meta.id` always stays the original session id — handy for tracking across restarts.
2. **Truncation tolerance.** Torn last line gets a warning, not a fatal error. This is the simplified version of upstream's `recover_from_partial_write`.
3. **`O_APPEND` + mutex** is not redundant. `O_APPEND` makes one `write(2)` syscall atomic, but multiple `write` calls can interleave; the mutex ensures "one line = one write".
4. **No fsync.** Upstream typically doesn't fsync per-record either — too slow. We accept losing the last 1-2 lines on a power loss.

## What Changed (vs. s06)

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

s05/s06 don't need to import s07 — but s10 (the integration session) wires Recorder into the main codex.events path.

## Try It

```bash
cd agents/s07-rollout

# Record a demo session
go run ./cmd -mode=record -path=/tmp/learn-codex.jsonl
cat /tmp/learn-codex.jsonl
# you'll see three JSON lines: session_meta / user_input / assistant_text

# Replay
go run ./cmd -mode=replay -path=/tmp/learn-codex.jsonl

go test -v ./...
```

## Upstream Source Reading

```upstream:codex-rs/rollout/src/recorder.rs
// Source: codex-rs/rollout/src/recorder.rs (excerpt)

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

**Reading notes**:

- **Async channel + writer task.** Upstream has a separate task that writes to disk; the main thread queues via a channel. We do mutex+write directly — simpler code, but main-thread write is synchronous. Production wants async.
- **`CompactedItems` variant.** Upstream proactively compacts (summarising old tool results into one sentence) to save tokens. We skip.
- **`EventPersistenceMode`.** Decides whether EventMsgs get persisted. SaveAll for debug replays, None for normal users. We default to SaveAll.
- **`list_sessions`.** Upstream has a resume picker UI (`codex resume` shows the list). s10 builds the minimal version.
- **Periodic fsync.** Upstream doesn't fsync per record but does periodically (every second or N records). We don't fsync at all.

**Read further**: start at `recorder.rs::record_items`, follow `writer_task` into `RolloutWriterTask::run` to see batch writing and channel-full handling; then look at the `record_items` callsites in `core/src/codex.rs::handle_event`. That's the s07 → s10 trace.

---

**Next**: s08 puts a real OS sandbox in front of s03's shell tool — macOS Seatbelt at the kernel layer, Linux Landlock at the syscall layer.
