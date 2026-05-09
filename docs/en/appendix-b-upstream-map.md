---
title: "Appendix B · upstream codex-rs source-reading map"
chapter: B
slug: appendix-b-upstream-map
est_read_min: 12
---

# Appendix B · upstream codex-rs source-reading map

> You've finished learn-codex's 10 chapters. To read the real `openai/codex`, start where? This is a "what to read in what order" map.

---

## Reading order

Top to bottom, every file is < 1500 LOC and lives in `codex-rs/`:

```
1. codex-rs/protocol/src/protocol.rs           ← protocol skeleton (you read s01's excerpt)
2. codex-rs/core/src/codex_thread.rs           ← the loop: submit + next_event
3. codex-rs/core/src/codex.rs                  ← Codex's internal state machine; dispatches all ops
4. codex-rs/core/src/client.rs                 ← Responses API client + WS fallback
5. codex-rs/core/src/exec.rs                   ← you read s03's excerpt
6. codex-rs/core/src/exec_policy.rs            ← you read s06's excerpt (Starlark integration layer)
7. codex-rs/execpolicy/src/lib.rs              ← full Starlark policy engine
8. codex-rs/apply-patch/src/lib.rs             ← V4A parse + apply
9. codex-rs/core/src/guardian/mod.rs           ← nested-model approval (learn-codex skipped)
10. codex-rs/rollout/src/recorder.rs           ← you read s07's excerpt
11. codex-rs/sandboxing/src/lib.rs             ← per-OS sandbox dispatch
12. codex-rs/linux-sandbox/src/landlock.rs     ← Landlock syscall wrapping
13. codex-rs/mcp-client/src/lib.rs             ← stdio + http MCP client
14. codex-rs/exec/src/lib.rs                   ← headless frontend
15. codex-rs/tui/src/lib.rs                    ← Ratatui frontend
16. codex-rs/cli/src/main.rs                   ← clap subcommand surface
```

**First pass**: only read trait/struct definitions and public method signatures. Skip implementations. 30 minutes for the whole list. Goal: build the shape map.

**Second pass**: pick whichever file you're **least sure about** and read it end to end. Drive your reading with the question "why not X?" — upstream made specific calls; you should hold candidates in mind.

**Third pass**: trace one user request through the stack. `codex-rs/cli/src/main.rs::main` → `tui::run` → `CodexThread::submit(Op::UserInput)` → `codex_thread::run_turn` → `client.stream` → `responses_stream::parse` → emit EventMsg → `tui::render_event`. After this pass you can whiteboard the architecture for a coworker.

---

## Each session ↔ which upstream files

| learn-codex session | Primary upstream | Secondary upstream |
|---|---|---|
| s01 minimum-loop | `codex-rs/protocol/src/protocol.rs`, `codex-rs/core/src/codex_thread.rs` | `codex-rs/protocol/src/lib.rs`, `codex-rs/core/src/codex.rs` |
| s02 model-client | `codex-rs/core/src/client.rs`, `codex-rs/model-provider/src/lib.rs` | `codex-rs/chatgpt/src/chatgpt_client.rs`, `codex-rs/protocol/src/responses_api.rs` |
| s03 exec-tool | `codex-rs/core/src/exec.rs` | `codex-rs/exec-server/src/lib.rs` |
| s04 apply-patch | `codex-rs/apply-patch/src/lib.rs` | `codex-rs/apply-patch/src/parser.rs` (if split) |
| s05 approval | `codex-rs/core/src/codex_thread.rs` (approval branch) | `codex-rs/core/src/guardian/mod.rs` |
| s06 execpolicy | `codex-rs/core/src/exec_policy.rs` | `codex-rs/execpolicy/src/lib.rs`, `codex-rs/execpolicy/src/heuristics.rs` |
| s07 rollout | `codex-rs/rollout/src/recorder.rs`, `codex-rs/rollout/src/lib.rs` | `codex-rs/message-history/src/lib.rs` |
| s08 sandbox | `codex-rs/sandboxing/src/lib.rs`, `codex-rs/sandboxing/src/seatbelt.rs` | `codex-rs/linux-sandbox/src/landlock.rs`, `codex-rs/windows-sandbox-rs/src/lib.rs`, `codex-rs/bwrap/src/lib.rs` |
| s09 mcp-bridge | `codex-rs/mcp-client/src/lib.rs`, `codex-rs/core/src/mcp.rs` | `codex-rs/mcp-server/src/lib.rs` (the reverse: codex acting as a server) |
| s10 cli-driver | `codex-rs/cli/src/main.rs`, `codex-rs/exec/src/lib.rs` | `codex-rs/tui/src/lib.rs`, `codex-rs/tui/src/slash_commands.rs` |

---

## 5 extension exercises

After the 16 files above, pick whichever interests you most.

### Exercise 1 · Swap Chat Completions for OpenAI Responses API

learn-codex s02 uses CC. Replace `OpenAIChatCompletions` with `OpenAIResponsesAPI`. Differences cluster in two places:

- Request body: `/v1/responses` accepts `messages` *or* `previous_response_id`; the latter lets the server maintain the thread.
- SSE shape: `event: response.text.delta\ndata: {...}` instead of `data: {choices:[{delta:{...}}]}`. `response.tool_use.input.delta` replaces the `tool_calls.function.arguments` chunked accumulation.

After this your learn-codex can see `Reasoning` streams — try rendering reasoning as faint grey secondary messages.

### Exercise 2 · Implement context compaction

Upstream `codex-rs/core/src/compaction.rs`: when history nears the model's max tokens, take the oldest 5-10 tool call pairs and ask an independent LLM call to summarise them into a paragraph; replace the originals.

Approach:

1. Estimate token count before s02's `runTurn` (rough: `len(history) * 80`).
2. Over threshold → call `provider.Stream` separately with "summarise these 5 tool calls in one paragraph".
3. Replace the originals with one `RolloutItem{Kind: KindCompacted, Payload: …}`.
4. Handle `KindCompacted` correctly on resume.

This is core to codex's context handling — and the worst part of most agent frameworks. Master this and you've graduated from agent context engineering school.

### Exercise 3 · Implement guardian nested approval

Upstream `codex-rs/core/src/guardian/mod.rs`: when `approval_policy=Granular`, a risky tool call doesn't pop to the user; instead a **nested CodexThread** is spawned with the user intent summary + tool call details. An independent model call assesses risk and returns a structured `{risk_level, outcome, rationale}`.

Implementation:

1. In s05 Gate.Check, when policy=Granular, call a `subAgent.Run(ctx, "Should this command be allowed? user wanted X, model proposed Y, output...")` with strict JSON-schema input.
2. Parse the result, decide Allow / Deny.
3. If denial rate > 30%, downgrade to OnRequest.

This is the simplest case of an **agent calling another agent** — the same graph as "recursive self-improvement".

### Exercise 4 · Write a Bubble Tea TUI

learn-codex has no TUI; upstream uses Ratatui. Use [Bubble Tea](https://github.com/charmbracelet/bubbletea) (Go's Elm-architecture TUI library):

- Three columns: session list on the left (read `~/.codex-learn/sessions/*.jsonl` meta lines), chat history in the middle, exec output on the right.
- Smooth streaming agent message rendering (not line-by-line).
- `/policy` slash command pops a small modal to change ApprovalPolicy.

This is the key step from "teaching version" to "actually-usable local codex".

### Exercise 5 · Make learn-codex its own MCP server

s09 is the client. Reverse it: write an MCP **server** that exposes learn-codex's built-in tools (shell, apply_patch, sandbox check) to **other agents**.

Implementation:

1. A `cmd/learn-codex-mcp/main.go`: read JSON-RPC from stdin, respond per the MCP spec to `initialize` / `tools/list` / `tools/call`.
2. `tools/list` returns shell + apply_patch + a few built-in helpers (read_file, write_file, list_dir).
3. `tools/call` dispatches into s03 Run / s04 Apply.
4. Try `mcp.add learn-codex /path/to/learn-codex-mcp` from claude-code or any MCP-aware agent.

A practical "agents talking to agents" exercise — let other agents reuse your sandbox.

---

## Estimated effort per exercise

| Exercise | LOC | Time | Difficulty |
|---|---|---|---|
| 1. Responses API | ~150 (rewrite openai.go) | 4-8 h | medium |
| 2. Compaction | ~200 (s02 + s07) | 8-12 h | hard |
| 3. Guardian | ~250 (s05 + new module) | 12-20 h | hard (prompt engineering) |
| 4. Bubble Tea TUI | ~600 | 20-40 h | medium (long but mechanical) |
| 5. MCP server reverse | ~300 | 8-16 h | medium (spec-careful) |

---

## What NOT to read

For completeness: upstream has code that just adds confusion for newcomers.

- `codex-rs/cloud-tasks/`, `codex-rs/realtime-webrtc/`, `codex-rs/external-agent-sessions/` — internal infra, not the agent main path.
- `codex-rs/feedback/`, `codex-rs/analytics/`, `codex-rs/install-context/` — user-support / telemetry, irrelevant to learning the agent.
- `codex-rs/network-proxy/` — injects HTTP_PROXY into sandboxed commands; only meaningful after s08.
- All `*-test/` sub-crates — they're testing infrastructure, not the system under test.

Spend your time on the 16 files plus one extension exercise. Coming out the other side, you'll have a very clear sense of "how to write a production-grade agent".
