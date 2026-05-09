---
title: "附录 B · 上游 codex-rs 源码导读地图"
chapter: B
slug: appendix-b-upstream-map
est_read_min: 12
---

# 附录 B · 上游 codex-rs 源码导读地图

> learn-codex 的 10 节看完了。要去读真上游 `openai/codex` 仓库，从哪里开始？这一篇是按"先后该读什么"列的地图。

---

## 阅读顺序

按下面这条线从上往下读，每个文件不超过 1500 行，全在 `codex-rs/` 子目录里：

```
1. codex-rs/protocol/src/protocol.rs           ← 协议骨架（你已经读过 s01 摘录）
2. codex-rs/core/src/codex_thread.rs           ← 主 loop：submit + next_event
3. codex-rs/core/src/codex.rs                  ← Codex 的内部状态机；调度所有 op
4. codex-rs/core/src/client.rs                 ← Responses API 客户端 + WS fallback
5. codex-rs/core/src/exec.rs                   ← 你已经读过 s03 摘录
6. codex-rs/core/src/exec_policy.rs            ← 你已经读过 s06 摘录（Starlark 集成层）
7. codex-rs/execpolicy/src/lib.rs              ← 完整 Starlark policy 引擎
8. codex-rs/apply-patch/src/lib.rs             ← V4A parse + apply
9. codex-rs/core/src/guardian/mod.rs           ← 嵌套 model 审批（learn-codex 没做）
10. codex-rs/rollout/src/recorder.rs           ← 你已经读过 s07 摘录
11. codex-rs/sandboxing/src/lib.rs             ← OS 沙盒 dispatch
12. codex-rs/linux-sandbox/src/landlock.rs     ← Landlock syscall 包装
13. codex-rs/mcp-client/src/lib.rs             ← stdio + http MCP 客户端
14. codex-rs/exec/src/lib.rs                   ← headless 前端
15. codex-rs/tui/src/lib.rs                    ← Ratatui 前端
16. codex-rs/cli/src/main.rs                   ← clap subcommand surface
```

**第一遍**：每个文件**只读 trait/struct 定义 + 公开方法签名**。不读实现。这一遍 30 分钟看完，目的是把"形状地图"建起来。

**第二遍**：挑一个上面任何一个文件，从头读到尾。挑你**最不确定**的那一个。第二遍读时把"为什么 not X"作为驱动问题——上游做了一些选择，你心里要有候选 alternative。

**第三遍**：跟一条 user request 跑一次 stack trace。`codex-rs/cli/src/main.rs::main` → `tui::run` → `CodexThread::submit(Op::UserInput)` → `codex_thread::run_turn` → `client.stream` → `responses_stream::parse` → emit EventMsg → `tui::render_event`。这一遍读了之后你能给同事 white-board 整套架构。

---

## 每节学习 ↔ 哪些上游文件

| learn-codex session | 主要上游文件 | 次要上游文件 |
|---|---|---|
| s01 minimum-loop | `codex-rs/protocol/src/protocol.rs`, `codex-rs/core/src/codex_thread.rs` | `codex-rs/protocol/src/lib.rs`, `codex-rs/core/src/codex.rs` |
| s02 model-client | `codex-rs/core/src/client.rs`, `codex-rs/model-provider/src/lib.rs` | `codex-rs/chatgpt/src/chatgpt_client.rs`, `codex-rs/protocol/src/responses_api.rs` |
| s03 exec-tool | `codex-rs/core/src/exec.rs` | `codex-rs/exec-server/src/lib.rs` |
| s04 apply-patch | `codex-rs/apply-patch/src/lib.rs` | `codex-rs/apply-patch/src/parser.rs`（如果分文件） |
| s05 approval | `codex-rs/core/src/codex_thread.rs`（approval branch） | `codex-rs/core/src/guardian/mod.rs` |
| s06 execpolicy | `codex-rs/core/src/exec_policy.rs` | `codex-rs/execpolicy/src/lib.rs`，`codex-rs/execpolicy/src/heuristics.rs` |
| s07 rollout | `codex-rs/rollout/src/recorder.rs`, `codex-rs/rollout/src/lib.rs` | `codex-rs/message-history/src/lib.rs` |
| s08 sandbox | `codex-rs/sandboxing/src/lib.rs`, `codex-rs/sandboxing/src/seatbelt.rs` | `codex-rs/linux-sandbox/src/landlock.rs`, `codex-rs/windows-sandbox-rs/src/lib.rs`, `codex-rs/bwrap/src/lib.rs` |
| s09 mcp-bridge | `codex-rs/mcp-client/src/lib.rs`, `codex-rs/core/src/mcp.rs` | `codex-rs/mcp-server/src/lib.rs`（反方向：codex 自己作 server） |
| s10 cli-driver | `codex-rs/cli/src/main.rs`, `codex-rs/exec/src/lib.rs` | `codex-rs/tui/src/lib.rs`, `codex-rs/tui/src/slash_commands.rs` |

---

## 5 个延伸练习

读完上面 16 个文件之后，选一个你最感兴趣的练习：

### 练习 1 · 把 OpenAI Chat Completions 换成 OpenAI Responses API

学习版 s02 用 CC。把 `OpenAIChatCompletions` 换成 `OpenAIResponsesAPI`。差别集中在两处：

- 请求体：`/v1/responses` 的 input 是 `messages` *or* `previous_response_id`；后者让 server 维护 thread。
- SSE 形状：`event: response.text.delta\ndata: {...}` 而不是 `data: {choices:[{delta:{...}}]}`。`response.tool_use.input.delta` 替代 `tool_calls.function.arguments` chunked 累加。

完成后你的 learn-codex 能见到 `Reasoning` 流——尝试把 reasoning 渲染成淡灰色的次要消息。

### 练习 2 · 实现 context compaction

上游 `codex-rs/core/src/compaction.rs`：当 history 接近 model max tokens 时，挑最旧的 5-10 个 tool 调用对，让一个独立 LLM 调用把它们摘要成一句话，替换掉。

实现思路：

1. 在 s02 的 `runTurn` 之前估算 token 数（粗算 = `len(history) * 80`）。
2. 超过阈值 → 调一次单独的 `provider.Stream` 输入 "summarise these 5 tool calls in one paragraph"。
3. 用一条 `RolloutItem{Kind: KindCompacted, Payload: …}` 替换原来的 5 条。
4. resume 时正确处理 KindCompacted。

这是 codex 上下文管理的核心，也是绝大多数 agent 框架最差的部分。学透这个你 agent 上下文工程就出师了。

### 练习 3 · 实现 guardian 嵌套审批

上游 `codex-rs/core/src/guardian/mod.rs`：当 `approval_policy=Granular` 时，risky tool call 不直接弹给用户，而是 spawn 一个**嵌套的 CodexThread**——把 user intent 摘要 + tool call 详情塞进去，让一个独立模型 call 评估风险等级，返回结构化 `{risk_level, outcome, rationale}`。

实现：

1. 在 s05 Gate.Check 里，policy=Granular 分支调一个 `subAgent.Run(ctx, "Should this command be allowed? user wanted X, model proposed Y, output...")`，输入是 strict JSON schema。
2. parse 结果，决定 Allow / Deny。
3. 拒绝率 > 30% 时降级到 OnRequest。

这是 agent **调 agent** 的最简单 case，跟"recursive self-improvement"是一个 graph。

### 练习 4 · 写一个 Bubble Tea TUI

学习版没 TUI；上游用 Ratatui。用 [Bubble Tea](https://github.com/charmbracelet/bubbletea)（Go 的 Elm 架构 TUI）做一个：

- 三栏：左侧 session list（读 `~/.codex-learn/sessions/*.jsonl` 的 meta line），中间 chat history，右侧 exec output。
- streaming agent message 平滑渲染（不是一行一行）。
- `/policy` slash 命令弹小弹窗调 ApprovalPolicy。

这是把"教学版"做成"用得上的本地 codex"的关键一步。

### 练习 5 · 让 learn-codex 自己作 MCP server

s09 是 client。反过来：写一个 MCP **server** 暴露 learn-codex 的内置工具（shell、apply_patch、sandbox check）给**别的 agent** 用。

实现：

1. 一个 `cmd/learn-codex-mcp/main.go`：从 stdin 读 JSON-RPC，按 MCP spec 响应 `initialize` / `tools/list` / `tools/call`。
2. `tools/list` 返回 shell + apply_patch + 几个内置 codex helper（read_file, write_file, list_dir）。
3. `tools/call` dispatch 到 s03 Run / s04 Apply。
4. 在 claude-code 或者别的 MCP-aware agent 里 `mcp.add learn-codex /path/to/learn-codex-mcp` 试一下。

这是一个"agent 之间互联"的实战练习——让其他 agent 复用你的 sandbox。

---

## 每个练习预计花费

| 练习 | LOC | 时间 | 难度 |
|---|---|---|---|
| 1. Responses API | ~150 (重写 openai.go) | 4-8 小时 | 中 |
| 2. Compaction | ~200 (s02 + s07) | 8-12 小时 | 难 |
| 3. Guardian | ~250 (s05 + 新模块) | 12-20 小时 | 难（需要 prompt engineering） |
| 4. Bubble Tea TUI | ~600 | 20-40 小时 | 中（耐心活儿） |
| 5. MCP server 反向 | ~300 | 8-16 小时 | 中（spec 仔细） |

---

## 不要去读的部分

为了完整：上游有些代码，新人看了纯增加困惑。

- `codex-rs/cloud-tasks/`、`codex-rs/realtime-webrtc/`、`codex-rs/external-agent-sessions/` —— 都是 internal infra，不是 agent 主路径。
- `codex-rs/feedback/`、`codex-rs/analytics/`、`codex-rs/install-context/` —— 用户支持 / 遥测代码，与 agent 学习无关。
- `codex-rs/network-proxy/` —— 给 sandboxed 命令注 proxy 的，绕开 sandbox 网络限制。读懂 s08 之后才有意义。
- 所有 `*-test/` 子 crate —— 这是 testing infra，不是被测代码。

把时间留给上面 16 个文件 + 一个延伸练习。读完你对"如何写一个生产级 agent"会有非常清楚的感觉。
