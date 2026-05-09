---
title: "s_full · 端到端集成：16 步执行轨迹"
chapter: "s_full"
slug: s_full-integration
est_read_min: 15
---

# s_full · 端到端集成：16 步执行轨迹

> 本节不再加新代码——把 s01..s10 串起来，跟着一条用户请求穿过整个 stack，看 codex 真实是怎么"做事"的。

---

## 架构图（learn-codex 视角）

```
                              ┌──────────────────────────────────────────┐
   user types a line ────────▶│ s10  cmd/codex/main.go                   │
                              │   bufio.Scanner(stdin)                   │
                              │   slash.Match(line) ─→ /help /quit       │
                              │   else: runOneTurn(...)                  │
                              └──┬─────────────────────────────┬─────────┘
                                 │                             │
                                 │ Submit OpUserInput          │ rec.Record(KindUserInput)
                                 ▼                             ▼
                  ┌─────────────────────────────┐    ┌───────────────────────────┐
                  │ s02  Codex (goroutine)      │    │ s07  Recorder             │
                  │   draining `subs` channel   │    │   ~/.codex-learn/         │
                  │   per-turn:                 │    │     sessions/<id>.jsonl   │
                  │     emit EvTurnStarted      │    │   append-only JSONL       │
                  │     provider.Stream(...)    │    │   torn-line tolerant      │
                  └──┬──────────────────────────┘    └───────────────────────────┘
                     │
                     ▼
        ┌──────────────────────────────┐
        │ s02  OpenAIChatCompletions   │
        │   POST /v1/chat/completions  │  ◀──── HTTP SSE: text deltas + tool calls
        │   parseSSE → ProvText/...    │
        └──┬───────────────────────────┘
           │
           ▼ ProvToolCall("shell", args) ─────┐
           │                                    │
           │ EvToolCallRequested ──→ stdout      │ (in a fully wired version):
           │                                    ▼
           │                          ┌─────────────────────────────┐
           │                          │ s06 Policy.Classify(cmd)    │ → Allow|Prompt|Forbidden
           │                          └──┬──────────────────────────┘
           │                             │
           │                             ▼ (Prompt)
           │                          ┌─────────────────────────────┐
           │                          │ s05 Gate.Check(req, emit)   │ → emit ExecApprovalRequest
           │                          │                             │   wait Op::ExecApproval
           │                          └──┬──────────────────────────┘
           │                             │ (Allow)
           │                             ▼
           │                          ┌─────────────────────────────┐
           │                          │ s08 Sandbox.Wrap(cmd, perm) │ → sandbox-exec / Landlock
           │                          └──┬──────────────────────────┘
           │                             │
           │                             ▼
           │                          ┌─────────────────────────────┐
           │                          │ s03 Run() — exec.Cmd        │ → stdout/stderr deltas (cap 256 KB)
           │                          └──┬──────────────────────────┘
           │                             │ ExecResult
           │                             ▼
           │                       provider.Stream(... tool result ...) ─── next turn ───┐
           │                                                                              │
           │   For an "edit a file" tool call:                                            │
           │     ProvToolCall("apply_patch", patchText) ──→ s04 Parse + Apply             │
           │                                                                              │
           │   For an external MCP tool call:                                             │
           │     ProvToolCall("notion.search", args) ──→ s09 client.CallTool(...)         │
           │                                                                              │
           ▼                                                                              ▼
        EvTurnComplete  ◀────────── continues until model emits finish_reason="stop" ─────┘
```

---

## 16 步执行轨迹

**用户场景**：在 `~/proj/foo` 目录跑 `codex` 进交互模式，输入 `修一下 pkg/foo 那个失败的测试`。模型决定先看测试输出、再改代码、再 verify。

| # | Actor | learn-codex 文件 / 函数 | 上游对应 |
|---|---|---|---|
| 1 | User | `bufio.Scanner.Scan()` in `agents/s10-cli-driver/cmd/codex/main.go::cmdInteractive` | `tui/src/chatwidget.rs::Composer::on_keypress` |
| 2 | s10 | `slash.Match(line)` ↦ false（普通文本），进 `runOneTurn` | `tui/src/slash_commands.rs::SlashCommand::parse` ↦ None |
| 3 | s10 | `c.Submit(s02.OpUserInput{Text: line})` + `rec.Record(KindUserInput, ...)` | `CodexThread::submit(Op::UserInput{...})` |
| 4 | s02 | goroutine 在 `agents/s02-model-client/codex.go::run` 里 dequeue submission，进 `runTurn` | `core/src/codex_thread.rs::run_turn` |
| 5 | s02 | emit `EvTurnStarted` 到 `c.events`；`history.append(ChatMessage{Role:"user", Content:line})` | `EventMsg::TurnStarted` + history append |
| 6 | s02 | `provider.Stream(ctx, ProviderRequest{Model, Messages: history, Tools})` | `ModelClient::stream(...)` |
| 7 | s02 | `OpenAIChatCompletions.Stream` 发 HTTP `POST /v1/chat/completions`，stream:true | 上游走 Responses API + WebSocket |
| 8 | net | OpenAI 流回 SSE：text deltas → "I'll first run the test"，然后 tool_call name="shell" args="{\"cmd\":\"go test ./pkg/foo/...\"}" | identical 概念，shape 不同 |
| 9 | s02 | `parseSSE` 把 deltas 转 `ProvText` → `EvAgentMessage`；tool_call 累积成 `ProvToolCall` → `EvToolCallRequested` | `ResponseItem::Text/ToolUse` 进 EventMsg |
| 10 | s10 | `runOneTurn` 接到 `EvAgentMessage`：流式打 stdout + `rec.Record(KindAssistantText, …)` | `tui::render_agent_message` + rollout |
| 11 | s10 | `runOneTurn` 接到 `EvToolCallRequested`：(本节学习版) 仅打印 `[tool call: shell({...})]` 并记录到 rollout。**完整版**会进 step 12-14。 | `tool_dispatch::shell` 入口 |
| 12 | s06 | `(完整版)` `Policy.Classify(["go", "test", "./pkg/foo/..."])` → `(DecideAllow, &Rule{prefix:"go test"})` —— rules 文件命中 | `ExecPolicy::classify` |
| 13 | s05 | `(完整版)` 因为 Decide=Allow，`Gate.Check` 直接返回 `DecisionAllow`，**不**发 ExecApprovalRequest | `maybe_ask_for_approval` 短路 |
| 14 | s08 | `(完整版)` `Sandbox.Wrap(cmd, Permissions{ReadOnly:false, WritableRoots:[cwd], AllowNetwork:false})` —— darwin 生成 Seatbelt profile，linux stub | `SeatbeltSandbox::wrap` / `LandlockSandbox::wrap` |
| 15 | s03 | `(完整版)` `Run(ctx, ExecParams{Command:[...]}, deltas)` 起 child，pump stdout/stderr，cap 256 KB；deltas 实时发回前端 | `process_exec_tool_call` |
| 16 | s02 | tool 结果回喂：`history.append(ChatMessage{Role:"tool", ToolCallID:..., Content:"FAIL: ..."})` → 下一轮 `provider.Stream` → 模型决定发 `apply_patch` tool call → s04 `Parse` + `Apply` 写文件 → 再起一轮 `go test` → assistant 输出 "fixed" → `EvTurnComplete` | 同样的"feed result, next turn, repeat" 循环 |

学习版的 s10 实际上**没有**接到 12-15（s05/s06/s08）；但代码模式相同。把 `runOneTurn` 里 case `EvToolCallRequested` 那一节 expand 成：

```go
case s02.EvToolCallRequested:
    if e.Call.Function.Name == "shell" {
        var args struct{ Cmd string `json:"cmd"` }
        json.Unmarshal(e.Call.Function.Args, &args)
        cmd := []string{"sh", "-c", args.Cmd}

        // s06 — classify
        decision, rule := policy.Classify(cmd)
        if decision == s06.DecideForbidden { ... }

        // s05 — approve if needed
        gate := s05.NewGate(s05.ApprovalUnlessTrusted)
        gate.Trusted = func(c []string) (bool, string) {
            d, _ := policy.Classify(c)
            return d == s06.DecideAllow, ""
        }
        if gate.Check(ctx, s05.Request{ID: e.Call.ID, Command: cmd}, emit) == s05.DecisionDeny { ... }

        // s08 — sandbox
        wrapped, _ := s08.New().Wrap(exec.CommandContext(ctx, cmd[0], cmd[1:]...), perm)

        // s03 — run + stream
        res := s03.Run(ctx, s03.ExecParams{Command: ...}, deltas)

        // feed result back to LLM
        c.Submit(...)
    }
```

这就是 learn-codex 留给读者的最大那个延伸练习——把 s10 的 import 从 2 个扩展到 8 个，沿着 step 11→12→13→14→15 把 dispatch 链补全。架构是对的，加约 100 行 glue 即可。

---

## 一些 codex 设计哲学的"啊哈"瞬间

读完这 10 节回头看 codex-rs，有几个原本看不懂的决定突然变得明显：

1. **为什么 `submit` / `next_event` 不是 `chat(prompt) -> Response`？** 因为前端要能在长 turn 里塞 interrupt、approve、reload-config 这些 op；返回值是单条 response 就把这扇门关死了。
2. **为什么 EventMsg 有 30 个变种？** 因为它**不是**给程序员设计的——它是给 UI 设计的。frontend 想画进度条就要 `ExecCommandBegin/OutputDelta/End`；想画 diff view 就要 `PatchApplyBegin/End`；想画 toast 就要 `Error`。
3. **为什么 V4A 不是 unified diff？** 因为 LLM 善于复制粘贴，但**不善于数行号**。V4A 把"找位置"完全交给文本匹配，绕过了行号问题。
4. **为什么 approval × sandbox 是两个独立的 axis？** 因为它们防的不是同一类风险：approval 防"用户意图被绕过"，sandbox 防"代码 bug 或恶意输入"。CI 里 approval=Never + sandbox=ReadOnly 是合理的；本地交互 approval=OnRequest + sandbox=WorkspaceWrite 也是合理的。
5. **为什么 rollout 是 JSONL 不是 SQLite？** 因为 codex 的核心客群是 dev——`cat`/`grep`/`jq` 一份事故报告比打开 sqlite shell 快得多。可读性 > 检索效率。
6. **为什么 MCP 是独立进程？** 因为"codex 是一个 trust-boundary"。把第三方 plugin 关在子进程里，崩了不影响 codex；写得差只能用自己的 CPU 时间。
7. **为什么 codex 默认 talk to OpenAI Responses API 而不是 Chat Completions？** 因为 Responses API 暴露了 `Reasoning` 流（chain of thought），并且把 tool-use 状态机化得更好。这是 OpenAI-specific 优势。但是如果你想接 Anthropic / Ollama，Chat Completions 形状就够——这是为什么 model-provider crate 有 5 种实现而它们都只走 Chat Completions 的原因。

---

## 接下来读什么

- 读上游 `codex-rs/protocol/src/protocol.rs` 完整 EventMsg/Op 枚举。我们摘了 13/30。
- 读上游 `codex-rs/core/src/codex_thread.rs` 完整 `run_turn`。它负责协调 provider stream + tool dispatch + approval + sandbox + rollout 5 件事，~600 行。
- 读上游 `codex-rs/exec/src/lib.rs`：headless frontend，是 learn-codex `cmdExec` 的 100x 版本（处理 stdin JSON 输入、auto-deny approvals、structured stdout 等）。
- 读上游 `codex-rs/tui/src/lib.rs`：Ratatui TUI。learn-codex 故意没做这个；如果你想自己做，从 `tui/src/chatwidget.rs` 开始读。
- 写一个 learn-codex 自己的 MCP server（s09 反过来）——把 learn-codex 的 shell + apply_patch 暴露成 MCP tools，让别的 agent 用。
