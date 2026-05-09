# Curriculum plan: learn-codex

## Locked decisions

| Field | Value |
|---|---|
| Upstream | https://github.com/openai/codex |
| Target language | Go (1.26.3) |
| Chapter count | 10 (s01..s10) + s_full + appendix-a + appendix-b |
| Has LLM layer | yes — Phase G applies (multi-model addendum) |
| Module path | `github.com/Ding-Ye/learn-codex` |
| Shared package layout | `agents/sNN-<slug>/` self-contained Go modules tied via `go.work` |
| GitHub repo | Ding-Ye/learn-codex (public) |

## Curriculum table

| # | slug | title (zh) | title (en) | mechanism (upstream) | dependency | est. LOC |
|---|---|---|---|---|---|---|
| s01 | minimum-loop | 最小 agent loop：Op / EventMsg 协议 | Minimum loop: Op / EventMsg protocol | `codex-rs/protocol/src/protocol.rs`, `codex-rs/core/src/codex_thread.rs` | (none) | ~250 |
| s02 | model-client | 调用 Chat Completions 流式 + tool-use | Streaming Chat Completions + tool-use | `codex-rs/core/src/client.rs` | s01 | ~400 |
| s03 | exec-tool | shell 执行：流式输出 + 截断 + 超时 | Shell exec: streaming + cap + timeout | `codex-rs/core/src/exec.rs` | s01 | ~350 |
| s04 | apply-patch | V4A patch DSL 解析 + 应用 | V4A patch DSL parser + applier | `codex-rs/apply-patch/src/lib.rs` | s01 | ~450 |
| s05 | approval | 审批策略 + ExecApprovalRequest 往返 | Approval policy + ExecApprovalRequest round-trip | `codex-rs/core/src/codex_thread.rs` (approval branch) | s01, s03 | ~300 |
| s06 | execpolicy | 命令安全 DSL → Allow / Prompt / Forbidden | Shell-safety DSL → Allow / Prompt / Forbidden | `codex-rs/core/src/exec_policy.rs`, `codex-rs/execpolicy/` | s01, s05 | ~400 |
| s07 | rollout | JSONL 会话日志 + resume from disk | JSONL session log + resume from disk | `codex-rs/rollout/src/recorder.rs` | s01 | ~350 |
| s08 | sandbox | macOS Seatbelt + Linux Landlock 适配 | macOS Seatbelt + Linux Landlock adapters | `codex-rs/sandboxing/`, `codex-rs/linux-sandbox/` | s03 | ~400 |
| s09 | mcp-bridge | stdio JSON-RPC 调用外部 MCP server | stdio JSON-RPC client to external MCP server | `codex-rs/mcp-client/`, `codex-rs/core/src/mcp.rs` | s01 | ~450 |
| s10 | cli-driver | clap-style 子命令 + slash 命令 + 全集成 | clap-style subcommands + slash commands + glue | `codex-rs/cli/src/main.rs`, `codex-rs/exec/src/lib.rs` | all of s01–s09 | ~600 |
| s_full | integration | 端到端集成：16 步执行轨迹 | End-to-end integration: 16-step trace | (doc only — composes all sessions) | all | doc |
| App. A | mental-model | 附录 A · 本地优先 + Responses-vs-Chat-Completions + peer 比较 | Appendix A · Local-first + Responses-vs-Chat-Completions + peer comparison | mental model | (none) | doc |
| App. B | upstream-map | 附录 B · 上游 codex-rs 源码导读地图 | Appendix B · upstream codex-rs source-reading map | reference | (none) | doc |

Total est. LOC: ~3950 — well under 7000 budget. Each session is small enough that a fresh subagent can write it from this plan + the dossier.

## Shared types catalog (canonical for Go)

Every session uses **the same canonical types** but defines them inside its own module so chapters remain self-contained (no `agents/s01/` ← `agents/s02/` imports). Each module re-declares the minimal subset it needs; later sessions copy the type, augment it, and reference the prior session in docs.

```go
// ---- Wire protocol (defined in s01, re-declared as needed in later sessions) ----

// Op — frontend → backend submission. Tagged-union via a marker method.
type Op interface{ isOp() }

type OpUserInput     struct{ Text string }
type OpExecApproval  struct{ ID string; Approved bool }
type OpPatchApproval struct{ ID string; Approved bool }
type OpInterrupt     struct{}
type OpShutdown      struct{}

// EventMsg — backend → frontend event. Tagged-union via a marker method.
type EventMsg interface{ isEventMsg() }

type EvTurnStarted          struct{ TurnID string }
type EvAgentMessage         struct{ Text string }
type EvExecCommandBegin     struct{ ID string; Command []string }
type EvExecOutputDelta      struct{ ID string; Stream string; Bytes []byte }
type EvExecCommandEnd       struct{ ID string; ExitCode int }
type EvExecApprovalRequest  struct{ ID string; Command []string; Reason string }
type EvPatchApplyBegin      struct{ ID string; Path string }
type EvPatchApplyEnd        struct{ ID string; Success bool; Err string }
type EvPatchApprovalRequest struct{ ID string; Path string; PatchText string }
type EvTurnComplete         struct{ TurnID string }
type EvError                struct{ Message string }

// Submission — wraps one Op with id + (in real Codex) trace context.
type Submission struct {
    ID string
    Op Op
}

// ---- Provider abstraction (introduced in s02; multi-model in Phase G) ----

type Provider interface {
    // Stream sends `messages` + `tools` to the model and emits events on `out`.
    // Tool calls arrive as ProviderEventToolCall; the caller is responsible for
    // executing the tool and feeding the result back via a follow-up call.
    Stream(ctx context.Context, req ProviderRequest, out chan<- ProviderEvent) error
}

type ProviderRequest struct {
    Model    string
    Messages []ChatMessage
    Tools    []ToolSchema
}

type ChatMessage struct {
    Role    string  // "system" | "user" | "assistant" | "tool"
    Content string
    // For assistant messages with tool calls:
    ToolCalls []ToolCall
    // For tool messages:
    ToolCallID string
}

type ToolCall struct {
    ID       string
    Name     string
    ArgsJSON json.RawMessage
}

type ToolSchema struct {
    Name        string
    Description string
    Parameters  json.RawMessage // JSON Schema
}

type ProviderEvent interface{ isProvEvent() }

type ProvText      struct{ Delta string }
type ProvToolCall  struct{ Call ToolCall }
type ProvDone      struct{ FinishReason string }
type ProvError     struct{ Err error }

// ---- Tool abstraction (introduced in s03 for shell, reused in s04 for apply_patch) ----

type Tool interface {
    Name() string
    Schema() ToolSchema
    Execute(ctx context.Context, argsJSON json.RawMessage, emit func(EventMsg)) (resultJSON []byte, err error)
}

// ---- ExecParams (s03) ----

type ExecParams struct {
    Command          []string
    Cwd              string
    Env              map[string]string
    Timeout          time.Duration
    OutputCapBytes   int          // default 256 << 10
    SandboxLevel     SandboxLevel // s05 / s08 add real meaning
}

type SandboxLevel int
const (
    SandboxOff SandboxLevel = iota
    SandboxReadOnly
    SandboxWorkspaceWrite
    SandboxFullAccess
)

// ---- Approval policy (s05) ----

type ApprovalPolicy int
const (
    ApprovalNever ApprovalPolicy = iota
    ApprovalOnFailure
    ApprovalOnRequest
    ApprovalUnlessTrusted
    ApprovalGranular
)

// ---- Decision (s06) ----

type RuleDecision int
const (
    DecideAllow RuleDecision = iota
    DecidePrompt
    DecideForbidden
)

// ---- RolloutItem (s07) ----

type RolloutItem struct {
    Kind    string          // "session_meta" | "user_input" | "assistant_msg" | "tool_call" | "tool_result" | "event"
    TS      time.Time
    Payload json.RawMessage // shape depends on Kind
}
```

These signatures are **canonical**: every session re-declares the subset it uses, but field names and method names match. The `s_full` integration chapter glues them by importing each session's package (a thin wiring layer in `cmd/codex/main.go`).

## Per-session detail

### s01 — minimum-loop

- **Problem**: An LLM agent has no plumbing yet. We need a way for a frontend (CLI) to submit user input and receive a stream of events without coupling either side to the model.
- **Solution**: A `Codex` struct that owns `chan<- Submission` (in) + `<-chan EventMsg` (out). One goroutine drains submissions, "pretends" to be an agent (echoes back the input as `EvAgentMessage`), and emits `EvTurnStarted` / `EvTurnComplete` around it. This is the smallest version that reveals the protocol shape.
- **Code surface**: `agents/s01-minimum-loop/{op.go,event.go,codex.go,codex_test.go,cmd/main.go,go.mod,README.md,testdata/}`.
- **Tests** (3+):
  1. `TestSubmitProducesTurnStartedAndComplete` — submit one `OpUserInput`, observe `EvTurnStarted → EvAgentMessage → EvTurnComplete`.
  2. `TestInterruptStopsTurnEarly` — submit `OpUserInput` then `OpInterrupt`; observe `EvTurnComplete` with no `EvAgentMessage`.
  3. `TestShutdownClosesEventChannel` — submit `OpShutdown`; assert the event channel closes.
- **Upstream ref**: `codex-rs/protocol/src/protocol.rs` and `codex-rs/core/src/codex_thread.rs` — quote ~30 lines of the EventMsg/Op enums into `upstream-readings/s01-protocol.rs` with annotations.
- **What changed vs. previous session**: (none — first chapter).

### s02 — model-client

- **Problem**: s01 fakes the agent. Now we need to actually call an LLM, parse a streaming response, and route any tool calls it returns through the event bus.
- **Solution**: Implement a `Provider` interface with one impl: `OpenAIChatCompletions` (using `net/http` + SSE parser). On a `OpUserInput`, the loop calls `provider.Stream(...)`, forwards `ProvText` deltas as `EvAgentMessage` chunks, and surfaces any `ProvToolCall` as a (s03-onwards) tool dispatch hook.
- **Code surface**: `agents/s02-model-client/{provider.go,openai.go,sse.go,codex.go,codex_test.go,cmd/main.go,go.mod,testdata/sse_fixtures.txt}`.
- **Tests** (4+):
  1. `TestSSEParsesTextDeltas` — feed canned SSE bytes, assert `ProvText` events.
  2. `TestSSEParsesToolCall` — assert `ProvToolCall` with name + args JSON.
  3. `TestProviderForwardsAsAgentMessage` — wire provider into Codex, observe `EvAgentMessage`.
  4. `TestProviderErrorBecomesEvError` — fault-injection.
- **Upstream ref**: `codex-rs/core/src/client.rs` — fetch the actual `ModelClient::stream` method. Note in docs: *upstream uses Responses API; we use Chat Completions for teaching simplicity. The shape of the deltas differs but the architectural lesson is the same.*
- **What changed vs. s01**: the loop is no longer a "echo"; it now delegates to Provider and forwards real model output. The `Codex` struct gains a `provider Provider` field.

### s03 — exec-tool

- **Problem**: A coding agent without `shell` is a chatbot. We need a tool that runs commands, streams stdout/stderr back as events, caps the output, and respects a timeout — without blocking the agent loop on a long-running build.
- **Solution**: Implement `Tool` for `shell` using `os/exec.CommandContext`. Two goroutines tee stdout/stderr through ring-buffered byte channels. Hard cap at 256 KB; any extra is summarised as `[…N bytes truncated]`. Timeout via context. Output deltas surface as `EvExecOutputDelta`.
- **Code surface**: `agents/s03-exec-tool/{tool_shell.go,exec.go,output_cap.go,exec_test.go,go.mod,cmd/main.go,testdata/scripts/{slow.sh,big_output.sh,bad_exit.sh}}`.
- **Tests** (4+):
  1. `TestShellRunsAndCapturesOutput` — run `echo hello`, assert exit 0 + "hello" in capture.
  2. `TestShellStreamsDeltasInOrder` — capture deltas via emit fn.
  3. `TestShellCapsOutput` — generate 300 KB; assert capped at 256 KB + truncation marker.
  4. `TestShellTimeoutKills` — `sleep 10` with 100 ms timeout; assert SIGKILL + exit code -1.
- **Upstream ref**: `codex-rs/core/src/exec.rs`. Quote `process_exec_tool_call` and `ExecParams` definitions.
- **What changed vs. s02**: when the model returns a `ProvToolCall(name="shell")`, we now actually run the command instead of just logging it. Codex grows a `tools map[string]Tool` registry.

### s04 — apply-patch

- **Problem**: An LLM editing files needs an unambiguous patch format that can't fuzz-match wrongly. Unified diff is fragile because line numbers drift; we need codex's V4A DSL.
- **Solution**: Build a parser for the V4A grammar:
  ```
  *** Begin Patch
  *** Update File: path/to/file.go
  @@
   context line
  -old line
  +new line
  *** End Patch
  ```
  Plus `*** Add File:`, `*** Delete File:`, optional `*** Move to: <new>`. The applier reads the file, finds the chunk by exact match of context+old lines, replaces, and writes back atomically. Surface as a Tool ("apply_patch").
- **Code surface**: `agents/s04-apply-patch/{patch.go,parser.go,applier.go,tool_patch.go,patch_test.go,go.mod,testdata/{add.patch,update.patch,delete.patch,move.patch,malformed.patch}}`.
- **Tests** (5+): one per hunk type + a malformed-patch failure test + a context-mismatch failure test.
- **Upstream ref**: `codex-rs/apply-patch/src/lib.rs`. Quote the `Hunk` enum and one parser fn.
- **What changed vs. s03**: we now have *two* tools registered (`shell`, `apply_patch`). The Tool registry grows to a real registry with conflict detection.

### s05 — approval

- **Problem**: Running arbitrary shell or writing to disk is dangerous. We need a user-in-the-loop gate: the agent emits an `EvExecApprovalRequest`, the loop blocks the turn, the user replies with `OpExecApproval{Approved: bool}`, and only then does the tool actually run.
- **Solution**: Add `ApprovalPolicy` enum to Codex config. Before invoking shell or apply_patch, the loop checks policy and (if needed) emits an approval request, then awaits the matching `Op*Approval` op via a per-request channel. Implements a denial counter (3 consecutive denials → interrupt the turn).
- **Code surface**: `agents/s05-approval/{policy.go,gate.go,gate_test.go,go.mod,cmd/main.go,testdata/}`.
- **Tests** (4+):
  1. `TestPolicyNeverSkipsApproval`.
  2. `TestPolicyOnRequestEmitsApprovalRequest`.
  3. `TestApprovalApprovedRunsTool`.
  4. `TestThreeDenialsInterrupt`.
- **Upstream ref**: `codex-rs/core/src/codex_thread.rs` — find the approval-branch state machine; cite ~40 lines.
- **What changed vs. s04**: tool dispatch now goes through `gate.Check(ctx, params)` first.

### s06 — execpolicy

- **Problem**: Asking the user for every command is annoying. Codex pre-classifies commands via a rules DSL: `Allow` runs without prompting, `Prompt` triggers s05's approval flow, `Forbidden` errors out. We need a parser + evaluator.
- **Solution**: Define a tiny rule format (we don't replicate Starlark; we use a simple line-oriented format inspired by it):
  ```
  rule allow prefix "git status"
  rule allow prefix "ls"
  rule prompt prefix "rm"     reason "destructive"
  rule forbid prefix "sudo"   reason "privilege escalation"
  rule allow exact "go test ./..."
  ```
  Parse to `[]Rule`, evaluate against an incoming `[]string` command, return `RuleDecision`. Hook into s05's gate so `DecideAllow` skips approval and `DecideForbidden` auto-rejects.
- **Code surface**: `agents/s06-execpolicy/{rule.go,parser.go,evaluator.go,evaluator_test.go,go.mod,testdata/policies/{default.rules,strict.rules}}`.
- **Tests** (5+): one per decision type + precedence (longest-prefix wins) + parser malformation.
- **Upstream ref**: `codex-rs/core/src/exec_policy.rs` and `codex-rs/execpolicy/src/`. Quote one Starlark policy excerpt; explain why we simplified.
- **What changed vs. s05**: gate decisions now come from the policy, not hardcoded to `Prompt`.

### s07 — rollout

- **Problem**: A long agent session is irrecoverable if the process crashes. We need durable session state (one JSONL file per session) and a way to replay it on resume so a new Codex sees the same conversation history.
- **Solution**: `Recorder` opens `~/.codex-learn/sessions/<uuid>.jsonl` in append mode and writes one `RolloutItem` per submit/event/tool-call. `Replay(path) (history []RolloutItem, err error)` loads it back and feeds the messages to the provider so the next turn has full context.
- **Code surface**: `agents/s07-rollout/{rollout.go,recorder.go,replayer.go,rollout_test.go,go.mod,testdata/sessions/{minimal.jsonl,truncated.jsonl}}`.
- **Tests** (4+):
  1. `TestRecorderAppendsOneItemPerSubmit`.
  2. `TestReplayRestoresMessages`.
  3. `TestTruncatedFileRecoversWhatItCan`.
  4. `TestConcurrentWritesAreSerialized`.
- **Upstream ref**: `codex-rs/rollout/src/recorder.rs`. Quote the writer-task channel pattern.
- **What changed vs. s06**: Codex now wraps every submission and event through `recorder.Record(item)`. New CLI flag `--resume <path>`.

### s08 — sandbox

- **Problem**: Even with approval, running a model-emitted command unsandboxed on the user's machine is reckless. Codex sandboxes per-platform: macOS Seatbelt (via `sandbox-exec`), Linux Landlock (LSM syscalls). A teaching version must show how the per-platform adapter dispatches.
- **Solution**: Build a `Sandbox` interface with two implementations:
  - `darwinSandbox` — generates a Seatbelt profile from `SandboxPermissions`, prepends `sandbox-exec -f /tmp/<uuid>.sb` to the command.
  - `linuxSandbox` — calls `landlock_create_ruleset(2)` + `landlock_add_rule(2)` + `landlock_restrict_self(2)` via `golang.org/x/sys/unix`. Restricts the child to read-only most paths and read-write only `cwd`.
  - `noopSandbox` — used on windows + when `SandboxOff`.
- **Code surface**: `agents/s08-sandbox/{sandbox.go,sandbox_darwin.go,sandbox_linux.go,sandbox_other.go,profile_seatbelt.go,landlock_linux.go,sandbox_test.go,go.mod,cmd/main.go,testdata/}`.
- **Tests** (4+):
  1. `TestDarwinProfileForbidsNetwork` — generate profile, grep for `(deny network*)`.
  2. `TestDarwinSandboxBlocksWrite` (skipped on non-darwin) — try to write outside cwd, expect EPERM.
  3. `TestLinuxLandlockBlocksWrite` (skipped on non-linux) — same but via Landlock.
  4. `TestNoopJustRuns` — windows / fallback path.
- **Upstream ref**: `codex-rs/sandboxing/src/lib.rs` + `codex-rs/linux-sandbox/src/landlock.rs`. Both quoted.
- **What changed vs. s07**: s03's exec tool now wraps `Sandbox.Wrap(params)` before spawning.

### s09 — mcp-bridge

- **Problem**: Codex doesn't know about every external tool a user might want. The Model Context Protocol lets users register external servers (e.g. filesystem, github, sqlite) that expose JSON-RPC `tools/list` + `tools/call`. We need a client that spawns one, lists its tools, and bridges them as `Tool` instances.
- **Solution**: Implement a stdio JSON-RPC client. Spawn `npx -y @modelcontextprotocol/server-everything` (or any user-config server), do `initialize`, then `tools/list`, register each as a `Tool` whose `Execute` sends a `tools/call` and waits for the result. Wire it into Codex's tool registry alongside `shell` and `apply_patch`.
- **Code surface**: `agents/s09-mcp-bridge/{mcp.go,client.go,jsonrpc.go,tool_mcp.go,mcp_test.go,go.mod,cmd/main.go,testdata/fixtures/server_everything_tools_list.json}`.
- **Tests** (4+):
  1. `TestJSONRPCRequestResponse`.
  2. `TestMCPInitializeHandshake` — fake server.
  3. `TestMCPToolsListRegistersAll` — fake server returns 3 tools, assert 3 Tools registered.
  4. `TestMCPToolCallRoundTrip` — fake server echoes args.
- **Upstream ref**: `codex-rs/mcp-client/src/lib.rs`. Quote the JSON-RPC plumbing.
- **What changed vs. s08**: the tool registry has a third source (besides `shell`+`apply_patch`).

### s10 — cli-driver

- **Problem**: Sessions s01–s09 each run as standalone demos. We need a real `codex` binary with subcommands (`codex`, `codex exec`, `codex resume`, `codex apply`, `codex sandbox`, `codex mcp list`) that wires the seven mechanisms together.
- **Solution**: One Cobra-style CLI in `agents/s10-cli-driver/cmd/codex/`. The interactive default reads stdin, submits ops, prints events to stdout. `exec` is one-shot. `resume` calls into s07. `mcp list` calls into s09. Slash commands inside the interactive REPL: `/help`, `/quit`, `/policy`, `/sandbox`, `/tools`.
- **Code surface**: `agents/s10-cli-driver/{main.go,interactive.go,exec.go,resume.go,apply.go,sandbox.go,mcp.go,slashcmd.go,e2e_test.go,go.mod,testdata/}`.
- **Tests** (3+):
  1. `TestExecOneShot` — `codex exec "say hi"` with mock provider, assert "hi" in stdout.
  2. `TestResumeReplaysSession` — record session in s07 fixture, resume, assert history applied.
  3. `TestSlashHelpListsCommands`.
- **Upstream ref**: `codex-rs/cli/src/main.rs` and `codex-rs/exec/src/lib.rs`. Quote the clap subcommand definition.
- **What changed vs. s09**: this session imports types from s01–s09 (one of the few places we *do* import across sessions, because it's the integration). It is the only session whose go.mod replaces back to local sister modules via `replace` directives.

### s_full — integration

- **Architecture diagram**: ASCII mirroring the dossier's "codex-rs/core" diagram but with Go package names. Show the data flow: `cli/interactive.go` → `Codex.Submit(Op)` → loop goroutine → `Provider.Stream` → tool dispatch (`Gate.Check` → `Sandbox.Wrap` → `Tool.Execute`) → events back → `Recorder.Record`.
- **Body**: walk the dossier's 16-step trace through learn-codex packages, citing exact Go file:line for each step.
- **No new code**.

### Appendix A — mental model: local-first agent + Responses-vs-Chat-Completions + peer comparison

- **Sections** (4):
  1. Why "the agent runs in your terminal" matters (latency, privacy, offline, file-access).
  2. OpenAI Responses API vs Chat Completions: what we lose by using the simpler one.
  3. Peer comparison table: Codex vs Claude Code vs Cline vs Aider vs Cursor.
  4. The two-axis safety model: `approval_policy` × `sandbox_mode`.
- **Why an appendix**: these are *non-code* dimensions. Putting them in a regular session would be filler.

### Appendix B — upstream codex-rs source-reading map

- Reading order: protocol → core/codex_thread → core/client → core/exec → apply-patch → core/exec_policy → rollout → sandboxing → mcp-client → cli.
- Per upstream file → which learn-codex session quotes it.
- Suggested extension exercises (5):
  1. Replace `OpenAIChatCompletions` with the Responses API and observe what `ResponseItem` opens up.
  2. Implement compaction (drop old tool results when context gets close to model max).
  3. Add the `guardian` nested-session approval flow.
  4. Add a Bubble Tea TUI.
  5. Wire learn-codex up to be a real MCP server itself (using s09 in reverse).

## Risks & open questions

1. **Landlock requires Linux ≥ 5.13** — many test machines won't have it. Mitigation: in s08, gate the landlock test path with a `runtime.GOOS != "linux"` skip, plus a kernel-version probe; document fallback.
2. **`sandbox-exec` is technically deprecated on macOS** but still ships and works as of macOS 14. Document this in s08.
3. **MCP server-everything is npm-only**. The s09 test uses an in-process fake JSON-RPC server (no npx required) so CI can pass without Node.
4. **OpenAI Chat Completions endpoint changes** — pin to a known-stable model id (`gpt-4o-mini` is fine for teaching) and put it in `~/.codex-learn/config.toml` so swapping is a one-line change.
5. **Streaming SSE parsing edge cases** (CRLF vs LF, comment lines, retry headers). s02 should ship a small but rigorous SSE test fixture.
6. **Cross-platform exec output cap** — Windows lacks SIGKILL. Document `os.Process.Kill` as the equivalent in s03.

## Naming conventions

- Folder: `agents/sNN-<slug>/`
- Doc files: `docs/{zh,en}/sNN-<slug>.md`
- Upstream readings: `upstream-readings/sNN-<slug-prefix>.rs` (verbatim Rust + bilingual annotations)
- Module name (Go): `github.com/Ding-Ye/learn-codex/agents/sNN-<slug>`
- Workspace: `go.work` with `use ./agents/s01-minimum-loop` … through `./agents/s10-cli-driver`

## Phase G note

`has_llm_call_layer = true`. After s_full + appendices land, Phase G adds:
- `pkg/provider/anthropic.go` — Anthropic Messages API impl
- `pkg/provider/echo.go` — deterministic provider for tests (replays scripted events)
- Each session's `cmd/main.go` gets a `--provider` flag (`openai|anthropic|echo`)
- `docs/{zh,en}/multi-model.md` — "swap the LLM in 5 lines"
