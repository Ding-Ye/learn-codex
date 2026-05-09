# s02 · model client (streaming Chat Completions + tool-use)

s01's loop just echoed. s02 swaps the echo for a real LLM call.

- `Provider` interface (provider.go) — every later session uses this.
- `OpenAIChatCompletions` (openai.go) — POSTs to `/v1/chat/completions` with `stream: true`, parses SSE, surfaces `ProvText` / `ProvToolCall` / `ProvDone`.
- `Codex` (codex.go) — same loop shape as s01, but `runTurn` now calls `provider.Stream` and forwards events. Tool calls are surfaced as `EvToolCallRequested` but not yet executed (s03 does that).

## Run

```bash
cd agents/s02-model-client
OPENAI_API_KEY=sk-... OPENAI_MODEL=gpt-4o-mini go run ./cmd

# Tests use a fake SSE server, no key needed
go test -v ./...
```

## Upstream

- [`codex-rs/core/src/client.rs`](https://github.com/openai/codex/blob/main/codex-rs/core/src/client.rs) — `ModelClient` + `ModelClientSession`, talks to OpenAI's Responses API (we use Chat Completions for teaching simplicity).
- [`codex-rs/model-provider/src/lib.rs`](https://github.com/openai/codex/blob/main/codex-rs/model-provider/src/lib.rs) — the `ModelProvider` trait analogous to our `Provider`.
- See [`upstream-readings/s02-client.rs`](../../upstream-readings/s02-client.rs).
