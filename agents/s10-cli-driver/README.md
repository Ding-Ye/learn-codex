# s10 · CLI driver (the integration session)

The first session that imports siblings — `s02-model-client` for streaming + tool calls and `s07-rollout` for durable session logs.

- `Registry` (cli.go) — clap-style subcommand dispatcher: `codex / exec / resume / sessions`.
- `SlashSet` (slashcmd.go) — REPL-internal `/help`, `/quit`, …; `Match()` returns whether the line was handled.
- `cmd/codex/main.go` — binds:
  - `s02.OpenAIChatCompletions` → `s02.Codex` for the agent loop;
  - `s07.Recorder` → durable `~/.codex-learn/sessions/<id>.jsonl`;
  - `Registry` + `SlashSet` for the user surface.

## Run

```bash
cd agents/s10-cli-driver

# Tests for the CLI / slash plumbing (no API key needed)
go test -v ./...

# Real run (talks to OpenAI):
OPENAI_API_KEY=sk-... go run ./cmd/codex          # interactive REPL
OPENAI_API_KEY=sk-... go run ./cmd/codex exec "hi"
go run ./cmd/codex sessions                       # list rollout files
go run ./cmd/codex resume ~/.codex-learn/sessions/sess-xxx.jsonl
```

Inside the REPL: `/help`, `/quit`. (Add more in `slashcmd.go`.)

## Upstream

- [`codex-rs/cli/src/main.rs`](https://github.com/openai/codex/blob/main/codex-rs/cli/src/main.rs) — clap subcommand surface.
- [`codex-rs/exec/src/lib.rs`](https://github.com/openai/codex/blob/main/codex-rs/exec/src/lib.rs) — the headless `codex exec` path.
- [`upstream-readings/s10-cli.rs`](../../upstream-readings/s10-cli.rs).
