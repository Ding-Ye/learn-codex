# s07 · rollout (JSONL session log)

Append-only per-session JSONL file under `~/.codex-learn/sessions/<id>.jsonl`. Survives crashes; replays cleanly into a new agent on resume.

- `Recorder.Record(kind, payload)` — append one item; mutex-guarded so multiple goroutines can write safely.
- `Replay(path)` — read back all items, skipping torn last lines.
- `Reconstruct(items)` — extract the user/assistant/tool conversation in order, ready to splice into the model's `messages` array.

## Run

```bash
cd agents/s07-rollout
go run ./cmd -mode=record -path=/tmp/x.jsonl
go run ./cmd -mode=replay -path=/tmp/x.jsonl
go test -v ./...
```

## Upstream

- [`codex-rs/rollout/src/recorder.rs`](https://github.com/openai/codex/blob/main/codex-rs/rollout/src/recorder.rs).
- [`upstream-readings/s07-rollout.rs`](../../upstream-readings/s07-rollout.rs).
