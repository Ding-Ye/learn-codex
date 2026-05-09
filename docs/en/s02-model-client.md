---
title: "s02 · Streaming Chat Completions + tool-use"
chapter: 2
slug: s02-model-client
est_read_min: 14
---

# s02 · Streaming Chat Completions + tool-use

> What this teaches: replace s01's "echo" with a real LLM call. Introduce the `Provider` interface (every later session uses it), implement OpenAI Chat Completions SSE streaming + cross-chunk tool-call assembly.

---

## Problem

s01 erected the protocol skeleton, but `runTurn` just prefixed the user text with `"echo: "`. An agent that doesn't talk to an LLM isn't an agent.

The hard part isn't issuing the HTTP request — it's **stream demuxing**. OpenAI Chat Completions chops the response into dozens or hundreds of SSE chunks, each a JSON object. Text deltas are easy. Tool calls are not: a single tool call's `arguments` is a JSON string that OpenAI emits one token at a time — `{"cmd":"go` in one chunk, ` version"}` in the next. We have to reassemble these by `index` in memory and only emit a `ToolCall` once `finish_reason` arrives.

## Solution

Three layers:

1. **`Provider` interface** — `Stream(ctx, req, out chan<- ProviderEvent) error`. Every later session uses this. Phase G adds Anthropic Messages and an echo impl.
2. **`OpenAIChatCompletions` impl** — `POST /v1/chat/completions` with `stream: true`, scan `data: …` lines, parse each chunk's `delta.content` and `delta.tool_calls`.
3. **Cross-chunk tool-call accumulation** — `map[int]*ToolCall` keyed by index, plus `map[int]*strings.Builder` to grow the args string. Flush completed `ProvToolCall`s when `finish_reason != ""`.

Why Chat Completions, not the upstream Responses API? Responses API returns structured `ResponseItem`s (text/tool_use/reasoning) — that fuses "the protocol shape" with "OpenAI-proprietary streaming", which is the wrong abstraction to teach. Chat Completions is the LCD of all LLM APIs — once you read this, the Responses API is one config flip away.

## How It Works

```
   Codex.runTurn(ctx, "list /tmp")
       │
       │ history.append(ChatMessage{role:user, content:"list /tmp"})
       ▼
   provider.Stream(ctx, req, out)──HTTP POST──▶ /v1/chat/completions stream:true
                                                          │
                                                          ▼
                                            data: {"choices":[{"delta":{"content":"I"}}]}
                                            data: {"choices":[{"delta":{"content":"'ll run"}}]}
                                            data: {"choices":[{"delta":{"tool_calls":[…name=shell, args="{\"cmd\":\"ls"}]}]}
                                            data: {"choices":[{"delta":{"tool_calls":[…args=" /tmp\"}"]}]}
                                            data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}
                                            data: [DONE]
                                                          │
                                                          ▼
        out chan ◀── ProvText{Delta:"I"} ──▶ EvAgentMessage{Text:"I"}
        out chan ◀── ProvText{Delta:"'ll run"} ──▶ EvAgentMessage{...}
        out chan ◀── ProvToolCall{Call:{name:shell, args:`{"cmd":"ls /tmp"}`}} ──▶ EvToolCallRequested
        out chan ◀── ProvDone{FinishReason:"tool_calls"}
                                                          │
                                                          ▼
                          history.append(assistant + tool_calls); EvTurnComplete
```

Core 30 lines from [`agents/s02-model-client/openai.go`](https://github.com/Ding-Ye/learn-codex/blob/main/agents/s02-model-client/openai.go):

```go
toolByIndex := map[int]*ToolCall{}
argsBuf := map[int]*strings.Builder{}

for scanner.Scan() {
    line := scanner.Text()
    if !strings.HasPrefix(line, "data: ") { continue }
    payload := strings.TrimPrefix(line, "data: ")
    if payload == "[DONE]" { return nil }

    var chunk /* … */; json.Unmarshal([]byte(payload), &chunk)
    ch := chunk.Choices[0]

    if ch.Delta.Content != "" { out <- ProvText{Delta: ch.Delta.Content} }
    for _, tc := range ch.Delta.ToolCalls {
        c, ok := toolByIndex[tc.Index]
        if !ok { c = &ToolCall{Type: "function"}; toolByIndex[tc.Index] = c; argsBuf[tc.Index] = &strings.Builder{} }
        if tc.ID != "" { c.ID = tc.ID }
        if tc.Function.Name != "" { c.Function.Name = tc.Function.Name }
        if tc.Function.Args != "" { argsBuf[tc.Index].WriteString(tc.Function.Args) }
    }
    if ch.FinishReason != "" {
        for i := 0; i <= len(toolByIndex); i++ {
            c, ok := toolByIndex[i]; if !ok { continue }
            c.Function.Args = json.RawMessage(argsBuf[i].String())
            out <- ProvToolCall{Call: *c}
        }
        out <- ProvDone{FinishReason: ch.FinishReason}; return nil
    }
}
```

**4 non-obvious points**:

1. **Tool calls must be accumulated by `index`.** OpenAI can emit multiple parallel tool calls in one stream (`index: 0, 1, 2`). Their chunks interleave; concatenating in arrival order is wrong.
2. **Don't flush until `finish_reason`.** Before that, the args string isn't complete. Emitting early loses fields.
3. **1 MiB scanner buffer.** A V4A patch in args can be hundreds of KB; bufio.Scanner's default 64 KB isn't enough.
4. **The assistant entry in `history` must include `tool_calls`.** When you feed tool results back next turn, OpenAI requires the original assistant message to carry the `tool_calls` array. (Buried deep in OpenAI's docs.)

## What Changed (vs. s01)

```diff
 type Codex struct {
+    provider Provider
+    model    string
+    system   string
+    history  []ChatMessage
     subs   chan Submission
     events chan EventMsg
 }

 func (c *Codex) runTurn(ctx context.Context, turnID, text string) {
     c.events <- EvTurnStarted{TurnID: turnID}
-    c.events <- EvAgentMessage{TurnID: turnID, Text: "echo: " + text}
+    c.history = append(c.history, ChatMessage{Role: "user", Content: text})
+    out := make(chan ProviderEvent, 16)
+    go func() { errCh <- c.provider.Stream(ctx, req, out); close(out) }()
+    for ev := range out { /* forward as EvAgentMessage / EvToolCallRequested */ }
     c.events <- EvTurnComplete{TurnID: turnID}
 }

+type EvToolCallRequested struct { TurnID string; Call ToolCall }
```

Semantic shift: the protocol shape is unchanged, but each turn is now a real model dialogue; `history` carries multi-turn context; `EvToolCallRequested` is the hook s03 will use to actually run tools.

## Try It

```bash
cd agents/s02-model-client

# Real OpenAI (needs API key)
OPENAI_API_KEY=sk-... go run ./cmd
> list every Go file in the current dir
  [turn sub-1 started]
  I'll list the Go files in the current directory.
  [tool call: shell({"cmd":"ls *.go"})]
  [turn sub-1 done]

# Tests use a fake SSE server, no key needed
go test -v ./...
```

Expected shape: assistant text appears token-by-token (streaming, not as one block); tool calls show as `[tool call: name(args)]`; `[turn done]` last.

## Upstream Source Reading

```upstream:codex-rs/core/src/client.rs
// Source: codex-rs/core/src/client.rs (excerpt)
// Upstream uses the Responses API (structured ResponseItem stream) wrapped
// in a WebSocket with HTTP fallback. We use Chat Completions + plain HTTP —
// same shape, fewer moving parts.

pub struct ModelClient {
    auth: Arc<AuthManager>,
    provider: ModelProvider,            // ★ trait abstracts OpenAI/Bedrock/Ollama/...
    thread_id: String,
    session_id: String,
    ws_fallback: Arc<Mutex<WebSocketFallbackState>>,
}

impl ModelClient {
    pub async fn stream(
        &self,
        request: ResponsesApiRequest,
    ) -> Result<impl Stream<Item = ResponseItem>> {
        // 1. open WS connection (sticky via x-codex-turn-state header)
        // 2. on connection failure: fallback to HTTP SSE
        // 3. parse Responses API events into ResponseItem objects:
        //      - ResponseItem::Text { delta }
        //      - ResponseItem::ToolUse { id, name, input }
        //      - ResponseItem::Reasoning { delta }
        //      - ResponseItem::Done { finish_reason }
    }
}
```

**Reading notes**:

- **Responses API vs Chat Completions.** Upstream's `ResponseItem` includes a `Reasoning` block (chain-of-thought stream); Chat Completions doesn't surface it. The teaching version skips it.
- **WebSocket primary + HTTP fallback.** Upstream's `ws_fallback` handles network flaps; we use HTTP only. When you want a stickier connection, this is the section to revisit.
- **The provider trait really exists.** Upstream's `model-provider` crate defines a `ModelProvider` trait with separate impls for OpenAIChat, Bedrock, Ollama, etc. Our `Provider` interface mirrors this design exactly.
- **Sticky routing via `x-codex-turn-state`.** Upstream uses this header to pin requests for a turn to one backend instance. Skipped in learn-codex, but it's a load-bearing piece of any production LLM gateway.

**Read further**: start at `client.rs::stream`, follow `Stream<Item=ResponseItem>` into `core/src/codex_thread.rs::run_turn`, see how ResponseItem dispatches into EventMsg. That trace is the real-source map for s01 → s02 → s05.

---

**Next**: s03 catches the tool calls s02 surfaces (as `EvToolCallRequested`) and actually executes them as shell commands, streaming stdout/stderr back line by line.
