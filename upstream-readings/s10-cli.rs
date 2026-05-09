// =============================================================================
//  Upstream reading for s10 — CLI driver
//  Source: codex-rs/cli/src/main.rs            (clap subcommand surface)
//          codex-rs/exec/src/lib.rs            (headless exec frontend)
//          codex-rs/tui/src/slash_commands.rs  (REPL slash commands)
// =============================================================================

/// Source: codex-rs/cli/src/main.rs
#[derive(Parser)]
#[command(name = "codex", version, about = "OpenAI's coding agent")]
struct Cli {
    #[command(subcommand)]
    cmd: Option<Subcmd>,

    /// Path to a config.toml override.
    #[arg(short, long)]
    config_path: Option<PathBuf>,

    /// Inline override of one config key (e.g. -c model=gpt-5).
    #[arg(short = 'c', long = "config", value_parser=parse_kv)]
    config_overrides: Vec<(String, String)>,
}

#[derive(Subcommand)]
enum Subcmd {
    /// One-shot, headless. Reads stdin if --json -. Skips approvals.
    Exec(ExecArgs),
    Review(ReviewArgs),
    Login(LoginArgs),
    Logout,
    Mcp(McpArgs),         // mcp list | add | remove
    Plugin(PluginArgs),
    Apply(ApplyArgs),     // apply a patch from stdin or file
    Resume(ResumeArgs),
    Fork(ForkArgs),       // resume but in a NEW session
    Sandbox(SandboxArgs), // run a single command via the configured sandbox
    AppServer(AppServerArgs),
    Completion(CompletionArgs),
    Update,
    Features(FeaturesArgs),
}

#[tokio::main]
async fn main() -> Result<()> {
    let cli = Cli::parse();
    init_logging(&cli);
    let config = load_config(cli.config_path, cli.config_overrides).await?;

    match cli.cmd {
        // ★ Default: full TUI.
        None => tui::run(config).await,
        Some(Subcmd::Exec(a)) => exec::run(a, config).await,
        Some(Subcmd::Resume(a)) => resume::run(a, config).await,
        Some(Subcmd::Apply(a)) => apply::run(a, config).await,
        Some(Subcmd::Sandbox(a)) => sandbox::run(a, config).await,
        Some(Subcmd::Mcp(a)) => mcp_subcmd::run(a, config).await,
        // … one match arm per Subcmd variant
    }
}

// -----------------------------------------------------------------------------
// Source: codex-rs/exec/src/lib.rs (headless frontend)
// -----------------------------------------------------------------------------

pub async fn run(args: ExecArgs, config: Config) -> Result<()> {
    let codex = CodexThread::new(config).await?;

    // Reject approvals: in headless mode, print a clear error and bail.
    let on_approval_request = Box::new(|_req| ApprovalDecision::Deny);

    let prompt = args.prompt.unwrap_or_else(read_stdin);
    codex.submit(Op::UserInput { text: prompt }).await;

    while let Some(ev) = codex.next_event().await {
        match ev {
            EventMsg::AgentMessage { text, .. } => print!("{}", text),
            EventMsg::ExecCommandBegin { .. } => /* ignore — too verbose for headless */ {}
            EventMsg::ExecCommandEnd { exit_code, .. } => {
                if exit_code != 0 { eprintln!("[exec exited {}]", exit_code); }
            }
            EventMsg::ExecApprovalRequest { id, .. } => {
                codex.submit(Op::ExecApproval { id, approved: false }).await;
            }
            EventMsg::TurnComplete { .. } => break,
            EventMsg::Error { message } => { eprintln!("error: {}", message); break; }
            _ => {}
        }
    }
    Ok(())
}

// -----------------------------------------------------------------------------
// Source: codex-rs/tui/src/slash_commands.rs
// -----------------------------------------------------------------------------

pub enum SlashCommand {
    Help,
    Quit,
    Tools,           // /tools — list built-in + MCP tools
    Policy,          // /policy — show / change ApprovalPolicy at runtime
    Sandbox,         // /sandbox — show / change sandbox_mode
    Save,            // /save — force-flush the rollout
    Edit,            // /edit — open in-progress message in $EDITOR
    Compact,         // /compact — request context compaction
    Mention(String), // @path → quote a file's contents into the prompt
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
            "sandbox" => Some(Self::Sandbox),
            "save" => Some(Self::Save),
            "edit" => Some(Self::Edit),
            "compact" => Some(Self::Compact),
            _ => None,
        }
    }
}

// =============================================================================
// Comparison summary
//
//   Concept           | Upstream                                  | learn-codex (s10)
//   ------------------+-------------------------------------------+----------------------
//   subcommand parser | clap derive (Cli + Subcmd enums)          | hand-rolled Registry
//   default action    | tui::run() (Ratatui)                      | text REPL
//   subcommands       | 14 (exec/login/mcp/apply/resume/sandbox/…) | 3 (exec/resume/sessions)
//   slash commands    | 9 (/help /quit /tools /policy /sandbox /…) | 2 (/help /quit)
//   approval headless | auto-deny                                 | (no gate wired in s10)
//   config layers     | toml + -c overrides + per-profile          | env vars only
//   logging           | tracing subscriber per subcommand          | bare fmt
// =============================================================================
