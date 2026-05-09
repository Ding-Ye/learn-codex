// =============================================================================
//  Upstream reading for s03 — exec tool
//  Source: codex-rs/core/src/exec.rs
// =============================================================================

/// Source: codex-rs/core/src/exec.rs
pub struct ExecParams {
    pub command: Vec<String>,
    pub cwd: PathBuf,
    pub env_vars: HashMap<String, String>,
    pub network_proxy: Option<ProxyConfig>,
    pub sandbox_permission_level: SandboxPermissionLevel,
    pub expiration: ExecExpiration,
    pub output_cap_bytes: usize,
}

pub enum ExecExpiration {
    Timeout { millis: u64 },
    Cancellation { token: CancellationToken },
}

pub struct ExecResult {
    pub exit_code: i32,
    pub stdout: Vec<u8>,
    pub stderr: Vec<u8>,
    pub truncated: bool,
    pub sandbox_denied: bool,    // ← detected via stderr keyword scan
}

pub struct ExecCommandOutputDelta {
    pub stream: Stream,           // Stdout | Stderr
    pub bytes: Vec<u8>,
    pub seq: u32,
}

const BUF_SIZE: usize = 4096;
const MAX_DELTAS_PER_CALL: u32 = 10_000;
const DEFAULT_OUTPUT_CAP: usize = 256 * 1024;

/// The streaming exec entry point. Returns after the child has exited.
pub async fn process_exec_tool_call(
    params: ExecParams,
    delta_tx: Option<mpsc::Sender<ExecCommandOutputDelta>>,
) -> Result<ExecResult, ExecError> {
    // 1. Build tokio::process::Command from params (cwd, env, args).
    // 2. spawn() returns Child with stdout/stderr Pipes.
    // 3. Spawn two tokio::tasks (one per stream); each:
    //      - reads BUF_SIZE bytes at a time into a Vec<u8>
    //      - acquires the shared cap-counter mutex
    //      - if cap would be exceeded, take only what fits, set truncated=true,
    //        spawn a `io::copy(&mut reader, &mut io::sink())` to drain rest,
    //        return early.
    //      - else write to the per-stream Vec<u8>, send ExecCommandOutputDelta
    //        on delta_tx (if present and seq < MAX_DELTAS_PER_CALL).
    // 4. join both tasks.
    // 5. expiration handling:
    //      Timeout(ms)  → tokio::time::timeout(Duration::from_millis(ms), child.wait())
    //      Cancellation → select! between child.wait() and token.cancelled()
    //    On either: child.kill().await? before bailing.
    // 6. exit_code from child.wait().
    // 7. sandbox-denial keyword check: scan stderr for known strings
    //    ("Operation not permitted", "EACCES", "sandbox-exec: deny ...") and
    //    set sandbox_denied = true to surface a hint to the user.
}

// =============================================================================
// Comparison summary
//
//   Concept           | Upstream                       | learn-codex (s03)
//   ------------------+--------------------------------+--------------------------
//   spawn             | tokio::process::Command        | os/exec.CommandContext
//   pipe reader       | tokio::io::BufReader           | bufio.Reader (or raw)
//   per-task control  | spawn 2 tokio::tasks           | spawn 2 goroutines
//   shared cap        | Mutex<usize> + drain on overflow | sync.Mutex + io.Discard drain
//   timeout           | tokio::time::timeout           | context.WithTimeout
//   cancellation      | CancellationToken              | ctx.Done()
//   sandbox-denial    | stderr keyword scan            | (omitted; s08 reintroduces)
//   network_proxy     | inject HTTP_PROXY env           | (omitted)
// =============================================================================
