---
title: "M · Multi-model guide (OpenAI / Anthropic / Echo)"
chapter: M
slug: multi-model
est_read_min: 8
---

# M · Multi-model guide

> learn-codex's `Provider` interface is intentionally tiny: one `Stream(ctx, req, out) error`. This addendum demonstrates three implementations — OpenAI Chat Completions (default), Anthropic Messages, and Echo (deterministic for tests) — and shows **how to swap providers**.

---

## Mental model

s02's `Provider` is the slot every part of codex plugs into:

```go
type Provider interface {
    Stream(ctx context.Context, req ProviderRequest, out chan<- ProviderEvent) error
}
```

Anything implementing this interface can be the agent's brain. OpenAI, Anthropic, Ollama, a local echo — the upper-level `Codex` can't tell.

Why does the abstraction work? Because the two things that differ most are pinned **inside** each implementation:

1. **HTTP wire format.** OpenAI and Anthropic SSE shapes differ a lot. `OpenAIChatCompletions.parseSSE` and `AnthropicMessages.parseAnthropicSSE` each manage their own.
2. **Message schema.** OpenAI uses role-tagged messages; Anthropic pulls system out as a top-level field and uses typed content blocks. `convertMessagesToAnthropic` handles the translation.

`ProviderEvent` is the smallest output common subset (Text / ToolCall / Done / Error), so upper layers (`Codex.runTurn`) never need an if-else "which vendor?".

## The three impls at a glance

```go
// 1. OpenAI Chat Completions (default)
provider := s02.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

// 2. Anthropic Messages
provider := s02.NewAnthropic(os.Getenv("ANTHROPIC_API_KEY"))

// 3. Echo — deterministic, zero network
provider := s02.EchoText("Hi! Mocked reply.")
```

### OpenAI

`agents/s02-model-client/openai.go`:

- `POST /v1/chat/completions` with `stream:true`.
- SSE shape: `data: {"choices":[{"delta":{"content":"…"}}]}`.
- Tool call args accumulate by `index` across chunks.
- Default model: `gpt-4o-mini` (s10 overrides via `OPENAI_MODEL`).

### Anthropic

`agents/s02-model-client/anthropic.go`:

- `POST /v1/messages` with `stream:true`, headers `x-api-key` and `anthropic-version`.
- SSE shape: typed events — `event: content_block_start` / `content_block_delta` / `content_block_stop` / `message_stop`.
- Text flows through `text_delta`; tool calls go through two phases — `content_block_start` carries id+name, `content_block_delta(input_json_delta)` accumulates args, `content_block_stop` emits the `ProvToolCall`.
- System messages must move to the top-level `system` field, not the messages array (the most visible deviation from OpenAI).
- Tool results are sent as `role:"user"` + `content:[{type:"tool_result", tool_use_id:"…"}]`, vs OpenAI's `role:"tool"`.
- Recommended models: `claude-haiku-4-5-20251001` (fast), `claude-sonnet-4-5-20250929`, `claude-opus-4-5-20250929`.

### Echo

`agents/s02-model-client/echo.go`:

- No network. The `Script []ProviderEvent` is emitted in order, followed by a `ProvDone`.
- Use cases: unit tests, CI without paying for API tokens, demos that prove "the agent loop doesn't depend on a real model".
- Two sugar helpers: `EchoText("Hi")` and `EchoToolCall(id, name, argsJSON)`.

## Switching providers is one line

From s10's `cmd/codex/main.go` (excerpt):

```go
prov, err := newProvider()                       // ← this line picks the vendor
if err != nil { … }
c := s02.New(ctx, prov, model, system)           // codex doesn't know or care
```

Add a `--provider` flag:

```go
flag := flag.String("provider", "openai", "openai|anthropic|echo")
flag.Parse()

var prov s02.Provider
switch *flag {
case "openai":
    prov = s02.NewOpenAI(os.Getenv("OPENAI_API_KEY"))
case "anthropic":
    prov = s02.NewAnthropic(os.Getenv("ANTHROPIC_API_KEY"))
case "echo":
    prov = s02.EchoText("Mocked! Set --provider=openai for the real thing.")
}

c := s02.New(ctx, prov, model, system)           // unchanged
```

The whole dispatch chain (s03 exec / s04 patch / s05 approval / s07 rollout / s09 MCP) stays untouched — they read `EvAgentMessage` / `EvToolCallRequested`, which are vendor-agnostic.

## Upstream alignment

codex-rs uses the same abstraction: a `ModelProvider` trait in `codex-rs/model-provider/src/lib.rs` with 5 implementations (OpenAI, Bedrock, Ollama, LMStudio, Custom HTTP) all aligned on the same input/output shape. Difference: upstream uses Responses API as the "standard input" (more detailed), but non-OpenAI providers still translate to Chat Completions internally. learn-codex picks Chat Completions as the standard directly — one less translation layer.

`upstream-readings/s02-client.rs` already excerpts the `ModelProvider` trait.

## Three exercises

1. **Add an Ollama implementation.** `POST http://localhost:11434/api/chat`; SSE shape resembles OpenAI but isn't identical. ~80 LOC.
2. **Add a fail-over wrapper.** Write `FallbackProvider{Primary, Backup Provider}`: switch to Backup when Primary errors. ~40 LOC.
3. **Add a cost meter.** Write `MeteredProvider{Inner Provider}` that wraps another and counts tokens. This teaches "Provider as middleware chain".

---

The path to multi-model is just this: pin wire-format inside each Provider impl, use `ProviderEvent` as the output common subset, let upper layers read only that. codex / langchain / vercel ai sdk / openrouter all converge on this pattern.
