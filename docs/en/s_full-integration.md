---
title: "s_full · End-to-end integration: 16-step trace"
chapter: "s_full"
slug: s_full-integration
est_read_min: 15
---

# s_full · End-to-end integration: 16-step trace

> No new code in this chapter — string s01..s10 together and follow one user request through the entire stack to see how codex actually *does* anything.

---

## Architecture diagram (learn-codex view)

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

## 16-step execution trace

**Scenario**: in `~/proj/foo`, the user runs `codex` to enter the REPL and types `fix the failing test in pkg/foo`. The model decides to (a) read the test output, (b) edit the source, (c) verify.

| # | Actor | learn-codex file / function | Upstream counterpart |
|---|---|---|---|
| 1 | User | `bufio.Scanner.Scan()` in `agents/s10-cli-driver/cmd/codex/main.go::cmdInteractive` | `tui/src/chatwidget.rs::Composer::on_keypress` |
| 2 | s10 | `slash.Match(line)` returns false (plain text) → `runOneTurn` | `tui/src/slash_commands.rs::SlashCommand::parse` returns None |
| 3 | s10 | `c.Submit(s02.OpUserInput{Text: line})` + `rec.Record(KindUserInput, ...)` | `CodexThread::submit(Op::UserInput{...})` |
| 4 | s02 | the goroutine in `agents/s02-model-client/codex.go::run` dequeues the submission and enters `runTurn` | `core/src/codex_thread.rs::run_turn` |
| 5 | s02 | emits `EvTurnStarted` to `c.events`; `history.append(ChatMessage{Role:"user", Content:line})` | `EventMsg::TurnStarted` + history append |
| 6 | s02 | `provider.Stream(ctx, ProviderRequest{Model, Messages: history, Tools})` | `ModelClient::stream(...)` |
| 7 | s02 | `OpenAIChatCompletions.Stream` sends HTTP `POST /v1/chat/completions`, stream:true | upstream uses Responses API + WebSocket |
| 8 | net | OpenAI streams back SSE: text deltas → "I'll first run the test", then a tool_call name="shell" args="{\"cmd\":\"go test ./pkg/foo/...\"}" | identical concept, different shape |
| 9 | s02 | `parseSSE` turns deltas into `ProvText` → `EvAgentMessage`; the tool_call accumulates into `ProvToolCall` → `EvToolCallRequested` | `ResponseItem::Text/ToolUse` flowing into EventMsg |
| 10 | s10 | `runOneTurn` receives `EvAgentMessage`: streams to stdout + `rec.Record(KindAssistantText, …)` | `tui::render_agent_message` + rollout |
| 11 | s10 | `runOneTurn` receives `EvToolCallRequested`: in the **teaching version**, just prints `[tool call: shell({...})]` and records to rollout. The **fully wired version** drops into steps 12–14. | `tool_dispatch::shell` entry |
| 12 | s06 | (full) `Policy.Classify(["go", "test", "./pkg/foo/..."])` → `(DecideAllow, &Rule{prefix:"go test"})` — rules file matches | `ExecPolicy::classify` |
| 13 | s05 | (full) Decide=Allow, so `Gate.Check` returns `DecisionAllow` immediately and **doesn't** emit ExecApprovalRequest | `maybe_ask_for_approval` short-circuits |
| 14 | s08 | (full) `Sandbox.Wrap(cmd, Permissions{ReadOnly:false, WritableRoots:[cwd], AllowNetwork:false})` — darwin generates a Seatbelt profile, linux is the stub | `SeatbeltSandbox::wrap` / `LandlockSandbox::wrap` |
| 15 | s03 | (full) `Run(ctx, ExecParams{Command:[...]}, deltas)` spawns the child, pumps stdout/stderr in 4 KB chunks, caps at 256 KB; deltas stream back to the frontend | `process_exec_tool_call` |
| 16 | s02 | tool result is fed back: `history.append(ChatMessage{Role:"tool", ToolCallID:..., Content:"FAIL: ..."})` → next `provider.Stream` → model emits `apply_patch` tool call → s04 `Parse` + `Apply` writes the file → another `go test` turn → assistant outputs "fixed" → `EvTurnComplete` | same "feed result, next turn, repeat" loop |

The teaching s10 doesn't actually wire steps 12–15 (s05/s06/s08); but the code shape is identical. Expand the `case EvToolCallRequested` branch in `runOneTurn` into:

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

That's learn-codex's biggest reader exercise — extend s10's import set from 2 to 8 modules and complete the dispatch chain along step 11→12→13→14→15. The architecture is right; ~100 lines of glue is enough.

---

## A few "aha" moments about codex's design

After writing 10 chapters, several upstream design decisions become obvious in retrospect:

1. **Why `submit` / `next_event` instead of `chat(prompt) -> Response`?** Because the frontend needs to inject interrupt, approve, reload-config ops mid-turn. A single return value would close that door.
2. **Why does EventMsg have 30 variants?** Because it's **not** designed for programmers — it's designed for UIs. Frontends drawing a progress bar need `ExecCommandBegin/OutputDelta/End`; diff views need `PatchApplyBegin/End`; toast notifications need `Error`.
3. **Why isn't V4A unified diff?** Because LLMs are good at copy-paste but **bad at counting line numbers**. V4A delegates "find the spot" entirely to text matching, sidestepping the line-number problem.
4. **Why are approval × sandbox two independent axes?** Because they protect against different risks: approval guards "user intent gets bypassed", sandbox guards "code bug or malicious input". CI with `approval=Never + sandbox=ReadOnly` is sane; interactive with `approval=OnRequest + sandbox=WorkspaceWrite` is sane.
5. **Why JSONL rollout, not SQLite?** Codex's core users are devs — `cat`/`grep`/`jq` of an incident is faster than launching a sqlite shell. Readability > query efficiency.
6. **Why is MCP a separate process?** Because "codex is a trust boundary". Quarantining third-party plugins in subprocesses means a crash doesn't take codex down; bad code costs only its own CPU time.
7. **Why does codex default to OpenAI's Responses API instead of Chat Completions?** Because Responses API exposes `Reasoning` streams (chain of thought) and models tool-use better. That's an OpenAI-specific edge. But if you want to plug in Anthropic / Ollama, Chat Completions is enough — which is exactly why `model-provider` has 5 impls all over Chat Completions.

---

## What to read next

- Read upstream `codex-rs/protocol/src/protocol.rs` for the full EventMsg/Op enums. We took 13/30.
- Read upstream `codex-rs/core/src/codex_thread.rs::run_turn` — coordinates provider stream + tool dispatch + approval + sandbox + rollout in ~600 lines.
- Read upstream `codex-rs/exec/src/lib.rs`: the headless frontend — a 100x version of learn-codex's `cmdExec` (handles stdin JSON, auto-denies approvals, structured stdout, etc.).
- Read upstream `codex-rs/tui/src/lib.rs`: the Ratatui TUI. learn-codex deliberately skipped this — start at `tui/src/chatwidget.rs` if you want to build it yourself.
- Build a learn-codex MCP server (s09 in reverse) — expose learn-codex's `shell` + `apply_patch` as MCP tools so other agents can use them.
