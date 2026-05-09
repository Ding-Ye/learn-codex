---
title: "s10 · CLI 驱动：全集成"
chapter: 10
slug: s10-cli-driver
est_read_min: 11
---

# s10 · CLI 驱动：全集成

> 教什么：把前 9 节的小模块装订成一个真正可执行的 `codex` 二进制——这是第一次有跨 session 的 import。子命令、slash 命令、headless exec、resume 都在这里收口。

---

## Problem / 问题

s01–s09 每一节都是独立 Go module，跑测试一套一套过。但用户拿到的是**一个二进制**：`codex` 进交互、`codex exec "..."` 单发、`codex resume <file>` 接着上次。这一节的工作是把"分散的功能"按 UX 想象的形状装订起来。

## Solution / 解决方案

3 个 vary tiny pieces：

1. **`Registry`（cli.go）**——子命令分发器。clap 风格的 `Add(Subcommand{Name, Synopsis, Run})` + `Dispatch(args)`。`exec` / `resume` / `sessions` 三个子命令；裸跑（无子命令）走默认的交互 REPL。
2. **`SlashSet`（slashcmd.go）**——REPL 内部的斜杠命令分发。`/help` 列命令，`/quit` 退出。`Match(line)` 返回 `(handled, shouldQuit)`，主循环据此决定是否把这行送给 LLM。
3. **`cmd/codex/main.go`**——真的把前面节装起来：
   - `import s02 "…/s02-model-client"` —— Provider + Codex loop
   - `import s07 "…/s07-rollout"` —— Recorder + Replay
   - 启动时 `s02.New(ctx, openai, model, system)` + `s07.New(rollout-path, meta)`
   - 每个 turn：`Submit OpUserInput → forward EvAgentMessage/EvToolCallRequested to stdout & rec.Record(...)`

go.mod 用 `replace` 指 `../s02-model-client` 和 `../s07-rollout`，让 import 走本地 module 而不是 GitHub。这是 monorepo 标准做法。

## How It Works / 工作原理

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

interactive REPL 的循环：

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

**4 个非显然之处**：

1. **`go.mod replace`**——10 个独立 module 在一个 repo 里互相 import 时必须 replace；否则 `go get` 会去 github 找还没 publish 的 path。`go.work` 也能干这事，但每个 cmd build 时还是要 module-level replace。
2. **default == interactive**——`os.Args` 长度 0 时不走 Registry，直接进 `cmdInteractive`。这 mirror 了上游 `codex` 不带子命令进 TUI 的行为。
3. **未知 args 当 prompt**——`codex 给我一个 hello world` 不是合法子命令，但用户多半就是想发这条 prompt。我们 fallback 到 `cmdExec`。`exec / resume / sessions` 几个保留字外的 token 都 prompt。
4. **`runOneTurn` 同时写 stdout + recorder**——recorder 不是事后从 stdout 抓回来的，是和 emit 同步分发；这样断电一刻 stdout 没冲完但 recorder 通常已经 flush 了那一行。

## What Changed / 与 s09 的变化

```diff
+ // s10 是第一个 import 兄弟节点的 module
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

我们故意**没** import s03-s06、s08、s09。原因：本节意图是教"集成 pattern"——一个 import 链就够了。如果你要做完整集成（shell tool + apply_patch + sandbox + execpolicy + MCP），`runOneTurn` 里检测到 `EvToolCallRequested(name="shell")` 时调 `s03.Run(...)`、`s05.Gate.Check(...)`、`s06.Policy.Classify(...)` 等等，模式相同——是个清晰的 reader's exercise。

## Try It / 动手试一试

```bash
cd agents/s10-cli-driver

# CLI / slash 测试不需要 API key
go test -v ./...

# 真实 codex 体验：
export OPENAI_API_KEY=sk-...
go run ./cmd/codex                              # interactive
go run ./cmd/codex exec "hello, who are you?"   # one-shot
go run ./cmd/codex sessions                     # 列出已有 rollout
go run ./cmd/codex resume ~/.codex-learn/sessions/sess-xxx.jsonl
```

REPL 里：

```
> 你好
  Hi! How can I help with code today?
  [turn sub-1 done]
> /help
  slash commands:
    /help       list slash commands
    /quit       exit the REPL
> /quit
```

## Upstream Source Reading / 上游源码阅读

```upstream:codex-rs/cli/src/main.rs
// Source: codex-rs/cli/src/main.rs (节选)

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
// Source: codex-rs/tui/src/slash_commands.rs (节选)

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

**对照阅读要点**：

- **clap derive 宏**：上游用 `#[derive(Parser)]` 生成所有 `--help` 文本和子命令路由。我们手写一个最小 Registry——12 行就够。
- **`tui::run()` 是默认**：上游 default 走 Ratatui TUI；我们 default 走文本 REPL。语义对等，差在像素 vs 行。
- **slash 命令 12+ 个**：上游 `/tools` `/policy` `/sandbox` `/save` `/edit` `/compact` `/mention` 都在。学习版只放了 `/help` `/quit`——剩下的全是 readers' exercise，每个对应一个前面 session 的功能（`/policy` 调 s05，`/tools` 拉 s09 MCP 列表 + s03/s04 内置，etc.）。
- **`@path` mention 语法**：上游有"@path 把文件内容塞进 prompt"。这是 LLM agent 常见 pattern，跟 slash 一样靠 `parse(line)` 区分。
- **`exec::run`** 实际上不简单：要做"non-interactive 时碰到 approval 怎么办" — 上游直接 deny。学习版 s05 没有 approval gate 接进来，所以 exec 模式没这个问题。

**想读更多**：从 `cli/src/main.rs::main` 入手，跟着 `Subcmd` 进 `exec::run`，看 ExecArgs → Codex thread setup → render-events-as-stdout 这条线；再回头读 `tui/src/lib.rs::run_main` 的同位置代码——TUI 和 exec 两个 frontend 共享 70% codex_thread 调用代码，这就是为什么"frontend 是接口、core 是实现"的分层值得。

---

**下一节预告**（s_full）：不再加新代码——把这 10 个机制串成一个端到端的 16 步 trace，跟着 `codex "fix the failing test in pkg/foo"` 这一条命令穿过整个 stack。
