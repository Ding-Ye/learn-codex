---
title: "s10 · CLI driver: full integration"
chapter: 10
slug: s10-cli-driver
est_read_min: 11
---

# s10 · CLI driver: full integration

> What this teaches: bind the previous 9 sessions into a real `codex` binary — the first session that imports siblings. Subcommands, slash commands, headless `exec`, `resume` all converge here.

---

## Problem

s01–s09 each shipped a stand-alone Go module with passing tests. But the user gets **one binary**: `codex` for interactive, `codex exec "..."` for one-shots, `codex resume <file>` to pick up where they left off. This chapter binds the scattered pieces into the shape the user expects.

## Solution

Three small pieces:

1. **`Registry` (cli.go)** — clap-style subcommand dispatcher: `Add(Subcommand{Name, Synopsis, Run})` + `Dispatch(args)`. Subcommands `exec / resume / sessions`; bare invocation falls through to the interactive REPL.
2. **`SlashSet` (slashcmd.go)** — REPL-internal slash command dispatcher. `/help` lists, `/quit` exits. `Match(line)` returns `(handled, shouldQuit)`; the main loop uses that to decide whether to forward the line to the LLM.
3. **`cmd/codex/main.go`** — the real wiring:
   - `import s02 "…/s02-model-client"` — Provider + Codex loop.
   - `import s07 "…/s07-rollout"` — Recorder + Replay.
   - At startup: `s02.New(ctx, openai, model, system)` + `s07.New(rollout-path, meta)`.
   - Per turn: `Submit OpUserInput → forward EvAgentMessage / EvToolCallRequested to stdout & rec.Record(...)`.

`go.mod` uses `replace` to point at `../s02-model-client` and `../s07-rollout`. Standard monorepo move.

## How It Works

```
$ codex exec "summarise README"
       │
       ▼
   main.go: dispatch on os.Args[1]
       │
       │ "exec" → cmdExec(args=["summarise", "README"])
       ▼
   newProvider() → s02.OpenAIChatCompletions{key, endpoint}
   newRecorder() → s07.Recorder appending to ~/.codex-learn/sessions/sess-<ts>.jsonl
   c := s02.New(ctx, provider, "gpt-4o-mini", system)
   c.Submit(s02.OpUserInput{Text: "summarise README"})
       │
       ▼ (s02.Codex goroutine)
   provider.Stream(...) ──── HTTP SSE stream ───▶ ProvText / ProvToolCall
       │
       ▼ events to channel
   for ev := range c.Events():
       case EvAgentMessage: write to stdout       + rec.Record(KindAssistantText, …)
       case EvToolCallRequested: write annotation + rec.Record(KindAssistantToolCall, …)
       case EvTurnComplete: break
       │
       ▼
   c.Submit(s02.OpShutdown{}); drain remaining; exit
```

The interactive REPL loop:

```go
for {
    line := scanner.ReadLine()
    if line == "" { continue }
    if handled, quit := slash.Match(line, stdout); handled {
        if quit { break }
        continue
    }
    runOneTurn(ctx, c, rec, line, stdout)   // ← s02 codex + s07 recorder
}
```

**4 non-obvious points**:

1. **`go.mod replace`.** When 10 standalone modules in one repo import each other, you have to replace; otherwise `go get` will look for a published path on github. `go.work` does similar work, but per-module builds still need module-level `replace`.
2. **Default == interactive.** `len(os.Args) == 0` skips the Registry and goes straight to `cmdInteractive`, mirroring upstream `codex` (no subcommand → TUI).
3. **Unknown args become a prompt.** `codex give me a hello world` isn't a registered subcommand, but the user almost certainly means "send this prompt". We fall back to `cmdExec`. Anything not in `exec / resume / sessions` is treated as a prompt.
4. **`runOneTurn` writes stdout AND recorder simultaneously.** The recorder isn't reading-back from stdout — it's a parallel sink fed at emit time. So if a power loss flushes stdout half-way, the recorder usually has the line already.

## What Changed (vs. s09)

```diff
+ // s10 is the first module that imports sibling sessions
+ import (
+     s02 "github.com/Ding-Ye/learn-codex/agents/s02-model-client"
+     s07 "github.com/Ding-Ye/learn-codex/agents/s07-rollout"
+ )

+ type Subcommand struct { Name, Synopsis string; Run func(...) int }
+ type Registry struct { Cmds []Subcommand }
+ func (r *Registry) Dispatch(args []string, …) int

+ type Slash struct { Name, Help string; Run func([]string, io.Writer) bool }
+ type SlashSet struct { Cmds []Slash }
+ func (s *SlashSet) Match(line string, …) (handled, shouldQuit bool)

+ // cmd/codex/main.go: the actual binary
+ subcommands: exec / resume / sessions / (default interactive)
+ slash: /help /quit (extensible)
```

We deliberately **don't** import s03–s06, s08, s09. Reason: this chapter teaches the integration *pattern* — one import chain is enough. To wire in shell tool + apply_patch + sandbox + execpolicy + MCP, when `runOneTurn` sees `EvToolCallRequested(name="shell")` it calls `s03.Run(...)`, `s05.Gate.Check(...)`, `s06.Policy.Classify(...)`, etc. Same pattern; explicitly a reader exercise.

## Try It

```bash
cd agents/s10-cli-driver

# CLI / slash tests need no API key
go test -v ./...

# Real codex experience:
export OPENAI_API_KEY=sk-...
go run ./cmd/codex                              # interactive
go run ./cmd/codex exec "hello, who are you?"   # one-shot
go run ./cmd/codex sessions                     # list rollouts
go run ./cmd/codex resume ~/.codex-learn/sessions/sess-xxx.jsonl
```

In the REPL:

```
> hello
  Hi! How can I help with code today?
  [turn sub-1 done]
> /help
  slash commands:
    /help       list slash commands
    /quit       exit the REPL
> /quit
```

## Upstream Source Reading

```upstream:codex-rs/cli/src/main.rs
// Source: codex-rs/cli/src/main.rs (excerpt)

#[derive(Parser)]
#[command(name = "codex", version, about = "...")]
struct Cli {
    #[command(subcommand)]
    cmd: Option<Subcmd>,
    #[arg(short, long)]
    config_path: Option<PathBuf>,
}

#[derive(Subcommand)]
enum Subcmd {
    /// One-shot, headless mode (no TUI).
    Exec(ExecArgs),
    Review(ReviewArgs),
    Login(LoginArgs),
    Logout,
    Mcp(McpArgs),         // mcp list | add | remove
    Plugin(PluginArgs),
    Apply(ApplyArgs),
    Resume(ResumeArgs),
    Fork(ForkArgs),
    Sandbox(SandboxArgs),
    AppServer(AppServerArgs),
    Completion(CompletionArgs),
    Update,
    Features(FeaturesArgs),
}

#[tokio::main]
async fn main() -> Result<()> {
    let cli = Cli::parse();
    match cli.cmd {
        None => tui::run().await,                 // ← default: TUI
        Some(Subcmd::Exec(a)) => exec::run(a).await,
        Some(Subcmd::Resume(a)) => resume::run(a).await,
        // ...
    }
}
```

```upstream:codex-rs/tui/src/slash_commands.rs
// Source: codex-rs/tui/src/slash_commands.rs (excerpt)

pub enum SlashCommand {
    Help,
    Quit,
    Tools,           // /tools — list available tools (built-in + MCP)
    Policy,          // /policy — show / change ApprovalPolicy
    Sandbox,         // /sandbox — show / change SandboxMode
    Save,            // /save — force flush rollout
    Edit,            // /edit — open the in-progress message in $EDITOR
    Compact,         // /compact — request context compaction
    Mention(String), // @path → quote a file's contents
    // …
}

impl SlashCommand {
    pub fn parse(line: &str) -> Option<Self> {
        let rest = line.strip_prefix('/')?;
        let mut parts = rest.split_whitespace();
        match parts.next()? {
            "help" => Some(Self::Help),
            "quit" | "q" | "exit" => Some(Self::Quit),
            "tools" => Some(Self::Tools),
            "policy" => Some(Self::Policy),
            // …
            _ => None,
        }
    }
}
```

**Reading notes**:

- **clap derive macros.** Upstream uses `#[derive(Parser)]` to generate all the `--help` text and routing. We hand-write a minimal Registry — 12 lines does it.
- **`tui::run()` is the default.** Upstream defaults into a Ratatui TUI; we default to a textual REPL. Semantically equivalent, the difference is in pixels vs lines.
- **12+ slash commands.** Upstream has `/tools` `/policy` `/sandbox` `/save` `/edit` `/compact` `/mention` and more. We ship only `/help` and `/quit`; the rest are reader's exercises that each route to a previous session (`/policy` → s05, `/tools` → s09 MCP list + s03/s04 built-ins, etc.).
- **`@path` mention syntax.** Upstream has "@path inlines a file into the prompt". A common LLM agent pattern; same `parse(line)` shape as slash commands.
- **`exec::run` isn't trivial.** What if a non-interactive session hits an approval prompt? Upstream auto-denies. The teaching version of s05 isn't wired into s10's loop, so we sidestep this.

**Read further**: from `cli/src/main.rs::main`, follow `Subcmd` into `exec::run` — the `ExecArgs → Codex thread setup → render-events-as-stdout` trace. Then read `tui/src/lib.rs::run_main` for the equivalent block. The TUI and exec frontends share ~70% of codex_thread call code; that's why the "frontend is interface, core is implementation" split pays off.

---

**Next** (s_full): no new code — string the 10 mechanisms into a single end-to-end 16-step trace, following `codex "fix the failing test in pkg/foo"` through the entire stack.
