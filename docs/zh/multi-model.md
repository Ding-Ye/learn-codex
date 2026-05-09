---
title: "M · 多模型接入指南（OpenAI / Anthropic / Echo）"
chapter: M
slug: multi-model
est_read_min: 8
---

# M · 多模型接入指南

> learn-codex 的 `Provider` 接口刻意小：一个 `Stream(ctx, req, out) error`。本附章演示三个实现——OpenAI Chat Completions（默认）、Anthropic Messages、Echo（确定性测试）——并展示**怎么换提供商**。

---

## 心智模型

s02 的 `Provider` 是整个 codex 的"插槽"：

```go
type Provider interface {
    Stream(ctx context.Context, req ProviderRequest, out chan<- ProviderEvent) error
}
```

任何实现这个接口的对象，都能给 codex 当大脑。OpenAI、Anthropic、Ollama、本地 echo——上层 `Codex` 看不出区别。

为什么这个抽象 work？因为我们把"差异最大"的两件事压在了实现内部：

1. **HTTP wire format**——OpenAI 和 Anthropic 的 SSE 形状完全不同。`OpenAIChatCompletions.parseSSE` 和 `AnthropicMessages.parseAnthropicSSE` 各管各的。
2. **消息 schema**——OpenAI 用 messages 数组里的 role；Anthropic 把 system 拎出来当顶层字段，用 typed content blocks。`convertMessagesToAnthropic` 处理这层翻译。

ProviderEvent 是出口的最小公共子集（Text / ToolCall / Done / Error），所以上层（`Codex.runTurn`）从来不需要 if-else "看是哪家"。

## 三种实现一览

```go
// 1. OpenAI Chat Completions（默认）
provider := s02.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

// 2. Anthropic Messages
provider := s02.NewAnthropic(os.Getenv("ANTHROPIC_API_KEY"))

// 3. Echo — 确定性，零网络
provider := s02.EchoText("Hi! Mocked reply.")
```

### OpenAI

`agents/s02-model-client/openai.go`：

- `POST /v1/chat/completions` with `stream:true`。
- SSE 形状：`data: {"choices":[{"delta":{"content":"…"}}]}`。
- tool call args 按 `index` 跨 chunk 累积。
- 默认模型：`gpt-4o-mini`（s10 通过 `OPENAI_MODEL` 覆盖）。

### Anthropic

`agents/s02-model-client/anthropic.go`：

- `POST /v1/messages` with `stream:true`，header 必带 `x-api-key` 和 `anthropic-version`。
- SSE 形状：分段——`event: content_block_start` / `content_block_delta` / `content_block_stop` / `message_stop`。
- text 走 `text_delta`；tool call 走两段 —— `content_block_start` 拿 id+name，`content_block_delta(input_json_delta)` 拼 args，`content_block_stop` 时 emit `ProvToolCall`。
- 系统消息要拎到顶层 `system` 字段，不能进 messages（这是和 OpenAI 最显眼的差别）。
- tool result 用 `role:"user"` + `content:[{type:"tool_result", tool_use_id:"…"}]`，对比 OpenAI 的 `role:"tool"`。
- 推荐模型：`claude-haiku-4-5-20251001`（fast）、`claude-sonnet-4-5-20250929`、`claude-opus-4-5-20250929`。

### Echo

`agents/s02-model-client/echo.go`：

- 不联网。`Script []ProviderEvent` 的内容按顺序 emit，最后跟一个 `ProvDone`。
- 用途：单测、CI 不想付 API 费、demo "agent loop 不依赖真模型"。
- 两个 sugar：`EchoText("Hi")` 和 `EchoToolCall(id, name, argsJSON)`。

## 换 provider 只改一行

s10 的 `cmd/codex/main.go`（节选）：

```go
prov, err := newProvider()                       // ← 这一行决定哪家
if err != nil { … }
c := s02.New(ctx, prov, model, system)           // codex 不知道也不关心
```

加一个 `--provider` flag：

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

c := s02.New(ctx, prov, model, system)           // 没变
```

整条 dispatch 链（s03 exec / s04 patch / s05 approval / s07 rollout / s09 MCP）都不用动——它们读的是 `EvAgentMessage` / `EvToolCallRequested`，跟 provider 无关。

## 上游对照

codex-rs 也走完全一样的抽象：`ModelProvider` trait 在 `codex-rs/model-provider/src/lib.rs`，5 个实现（OpenAI、Bedrock、Ollama、LMStudio、Custom HTTP）都对齐相同的输入/输出 shape。差异：上游用 Responses API 做"标准 input"（更详细），但 ChatGPT 之外的 provider 仍然往 Chat Completions 翻译。学习版直接用 Chat Completions 当标准——少一层翻译。

`upstream-readings/s02-client.rs` 已经摘了 `ModelProvider` trait 定义。

## 三个练习

1. **加 Ollama 实现**：`POST http://localhost:11434/api/chat`，SSE shape 跟 OpenAI 像但不一样。约 80 LOC。
2. **加 fail-over wrapper**：写一个 `FallbackProvider{Primary, Backup Provider}`，Primary 失败时切 Backup。约 40 LOC。
3. **加 cost meter**：写一个 `MeteredProvider{Inner Provider}`，包一层并对 token 数计数。这能教你如何把 Provider 接口当成"中间件链"。

---

通往多模型的路就这一条：把 wire format 关进 Provider 实现里、用 `ProviderEvent` 当出口的最小公约数、上层只读这个。codex / langchain / vercel ai sdk / openrouter 全都是这个 pattern。
