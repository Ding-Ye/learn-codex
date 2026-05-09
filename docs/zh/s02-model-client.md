---
title: "s02 · 调用 Chat Completions 流式 + tool-use"
chapter: 2
slug: s02-model-client
est_read_min: 14
---

# s02 · 调用 Chat Completions 流式 + tool-use

> 教什么：把 s01 的"echo"换成真正的 LLM 调用。引入 `Provider` 接口（后面九节都靠它），实现 OpenAI Chat Completions 的 SSE 流式 + 跨 chunk 的 tool-call 拼接。

---

## Problem / 问题

s01 立起了协议骨架，但 `runTurn` 只是把用户文字加 `"echo: "` 前缀回写。一个 agent 不接 LLM 就不算 agent。

接 LLM 的难点不在"发 HTTP 请求"，而在**流式拆装**：OpenAI Chat Completions 用 Server-Sent Events 把响应切成几十上百个 chunk，每个 chunk 是一个 JSON。文本 delta 好处理，工具调用就麻烦——一次工具调用的 `arguments` 是个 JSON string，OpenAI 会按 token 切开发，比如 `{"cmd":"go` 一个 chunk、` version"}` 下一个 chunk。我们必须按 `index` 把这些碎片在内存里拼回来，等 `finish_reason` 出现才把完整的 `ToolCall` emit 出去。

## Solution / 解决方案

三层抽象：

1. **`Provider` 接口**——`Stream(ctx, req, out chan<- ProviderEvent) error`。后面所有 session 都通过它和模型对话。Phase G 会再加 Anthropic Messages 和 echo 实现。
2. **`OpenAIChatCompletions` 实现**——`POST /v1/chat/completions` with `stream:true`，按行扫 `data: …`，解析每个 chunk 的 `delta.content` 和 `delta.tool_calls`。
3. **跨 chunk 累积 tool calls**：用 `map[int]*ToolCall` keyed by index，加上 `map[int]*strings.Builder` 累加 args 字符串。`finish_reason != ""` 时 flush 出完整的 `ProvToolCall`。

为什么用 Chat Completions 而不是上游用的 Responses API？Responses API 返回结构化的 `ResponseItem`（text/tool_use/reasoning），把"协议形状"和"OpenAI 专属流式"绑在一起，对教学不友好。Chat Completions 是所有 LLM 抽象的最大公约数——读懂它，再读 Responses API 是一行配置之差。

## How It Works / 工作原理

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

核心 30 行（节选自 [`agents/s02-model-client/openai.go`](https://github.com/Ding-Ye/learn-codex/blob/main/agents/s02-model-client/openai.go)）：

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

**4 个非显然之处**：

1. **tool call 必须按 index 累积**——OpenAI 在同一个流里可能 emit 多个并发 tool call（`index: 0, 1, 2`）。chunk 与 chunk 之间这些 index 交错出现，单纯按出现顺序拼接是错的。
2. **`finish_reason` 才 flush**——在 `finish_reason: "tool_calls"` 出现之前，args 字符串还没拼完。提前 emit 会丢字段。
3. **buffer 1 MiB**——长 patch 的 args（一个 V4A diff）能上几百 KB，bufio.Scanner 默认 64 KB 不够。
4. **`history` 累加 assistant 消息要带 tool_calls**——下一轮要把 tool 结果回喂时，OpenAI 需要看到原始 assistant 消息里包含 tool_calls 数组（这条规则 OpenAI 文档藏得很深）。

## What Changed / 与 s01 的变化

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

语义变化：协议外形不变，但每个 turn 现在是一次真实模型对话；`history` 让多轮上下文持续；`EvToolCallRequested` 是 s03 工具执行的挂钩点。

## Try It / 动手试一试

```bash
cd agents/s02-model-client

# 真实 OpenAI（需要 key）
OPENAI_API_KEY=sk-... go run ./cmd
> 列出当前目录所有 Go 文件
  [turn sub-1 started]
  I'll list the Go files in the current directory.
  [tool call: shell({"cmd":"ls *.go"})]
  [turn sub-1 done]

# 测试用 fake SSE server，不需要 key
go test -v ./...
```

期望输出形态：assistant 文本以**流式**逐 token 出现（不是整段）；tool call 以一行 `[tool call: name(args)]` 显示；最后 `[turn done]`。

## Upstream Source Reading / 上游源码阅读

```upstream:codex-rs/core/src/client.rs
// Source: codex-rs/core/src/client.rs (节选)
// 上游用 Responses API（结构化 ResponseItem 流），还套了一层 WebSocket
// + HTTP fallback。我们用 Chat Completions + 单一 HTTP，形状相同但简化。

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

**对照阅读要点**：

- **Responses API vs Chat Completions**：上游 `ResponseItem` 包含 `Reasoning` 块（chain-of-thought 流），Chat Completions 不暴露。学习版省掉。
- **WebSocket 优先 + HTTP fallback**：上游的 `ws_fallback` 处理网络抖动。学习版只用 HTTP——一旦你想要更稳的连接，就回头读这一段。
- **provider trait 真的存在**：上游 `model-provider` crate 定义 `ModelProvider` trait，OpenAIChat / Bedrock / Ollama 各一份实现。我们的 `Provider` 接口完全对齐这个设计。
- **sticky routing via `x-codex-turn-state`**：上游通过 header 让请求路由到同一个后端实例，保证一个 turn 内的状态一致。学习版省掉，但这是生产级 LLM 网关都要做的事情。

**想读更多**：从 `client.rs::stream` 入手，跟着 `Stream<Item=ResponseItem>` 进 `core/src/codex_thread.rs::run_turn`，看 ResponseItem 怎么 dispatch 成 EventMsg。这条线是 s01 → s02 → s05 的真实代码地图。

---

**下一节预告**：s03 把模型抛出的 tool call（s02 当作 EvToolCallRequested 摆出来）真正接住，跑成 shell 命令，按行流式回传 stdout/stderr。
