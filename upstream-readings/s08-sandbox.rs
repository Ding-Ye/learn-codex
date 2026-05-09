// =============================================================================
//  Upstream reading for s08 — sandbox adapters
//  Source: codex-rs/sandboxing/src/lib.rs        (SandboxManager + per-OS dispatch)
//          codex-rs/sandboxing/src/seatbelt.rs   (macOS)
//          codex-rs/linux-sandbox/src/landlock.rs (Linux)
//          codex-rs/windows-sandbox-rs/src/lib.rs (Windows restricted token)
// =============================================================================

/// Source: codex-rs/sandboxing/src/lib.rs
pub struct SandboxManager;

impl SandboxManager {
    pub fn get_platform_sandbox(perm: SandboxPermissions) -> Box<dyn Sandbox> {
        #[cfg(target_os = "macos")]
        return Box::new(SeatbeltSandbox::new(perm));
        #[cfg(target_os = "linux")]
        return Box::new(LandlockSandbox::new(perm));
        #[cfg(target_os = "windows")]
        return Box::new(WindowsSandbox::new(perm));
    }
}

pub trait Sandbox: Send + Sync {
    fn wrap(&self, cmd: Command) -> Result<Command, SandboxError>;
    fn name(&self) -> &str;
}

pub struct SandboxPermissions {
    pub filesystem: FilesystemPermission,    // Read | ReadWrite
    pub network: NetworkPermission,           // None | Restricted | Full
    pub writable_roots: Vec<PathBuf>,
    pub network_allowlist: Option<Vec<String>>,
}

// -----------------------------------------------------------------------------
// macOS: Seatbelt
// -----------------------------------------------------------------------------

/// Source: codex-rs/sandboxing/src/seatbelt.rs
pub struct SeatbeltSandbox { perm: SandboxPermissions }

impl Sandbox for SeatbeltSandbox {
    fn wrap(&self, cmd: Command) -> Result<Command, SandboxError> {
        let profile = self.build_profile();
        let path = write_temp_profile(&profile)?;

        let mut wrapped = Command::new("/usr/bin/sandbox-exec");
        wrapped.arg("-f").arg(&path);
        wrapped.arg(cmd.get_program());
        wrapped.args(cmd.get_args());
        wrapped.env_clear().envs(cmd.get_envs());

        // Note: sandbox-exec is technically deprecated by Apple, but still
        // ships and works as of macOS 14. Codex reckons that's acceptable.
        Ok(wrapped)
    }
    fn name(&self) -> &str { "seatbelt" }
}

impl SeatbeltSandbox {
    fn build_profile(&self) -> String {
        // (version 1)
        // (deny default)
        // (allow process-fork) (allow process-exec)
        // (allow signal (target self))
        // (allow file-read*)
        // for path in writable_roots:
        //     (allow file-write* (subpath "<path>"))
        // (allow mach-lookup (global-name "com.apple.cfprefsd.*"))
        // (allow mach-lookup (global-name "com.apple.mDNSResponder*"))
        // (allow network* (remote ip "localhost:*"))
        // ★ if AllowNetwork: (allow network*)
        // ★ if Restricted:   (allow network* (remote tcp ...allowlist))
    }
}

// -----------------------------------------------------------------------------
// Linux: Landlock
// -----------------------------------------------------------------------------

/// Source: codex-rs/linux-sandbox/src/landlock.rs
pub struct LandlockSandbox {
    abi: LandlockAbi,
    perm: SandboxPermissions,
}

impl Sandbox for LandlockSandbox {
    /// On Linux we can't sandbox the parent process — Landlock applies
    /// per-thread. Instead we wrap the command in a shim binary that calls
    /// `restrict_self()` BEFORE exec()ing the real command.
    fn wrap(&self, cmd: Command) -> Result<Command, SandboxError> {
        let mut wrapped = Command::new(landlock_wrapper_binary());
        wrapped.arg("--writable").args(&self.perm.writable_roots);
        if !self.perm.allow_network() { wrapped.arg("--no-network"); }
        wrapped.arg("--").arg(cmd.get_program()).args(cmd.get_args());
        Ok(wrapped)
    }
    fn name(&self) -> &str { "landlock" }
}

impl LandlockSandbox {
    /// Called by the wrapper binary, after fork/exec, before exec()ing the real cmd.
    pub fn restrict_self(&self) -> Result<(), LandlockError> {
        let ruleset_fd = landlock_create_ruleset(&self.attr())?;
        for path in &self.perm.writable_roots {
            landlock_add_rule(ruleset_fd, &path_beneath_attr(path, ALL_FS_RW_RIGHTS))?;
        }
        prctl(PR_SET_NO_NEW_PRIVS, 1)?;
        landlock_restrict_self(ruleset_fd, 0)?;
        Ok(())
    }
}

// -----------------------------------------------------------------------------
// Windows: Restricted Token
// -----------------------------------------------------------------------------

/// Source: codex-rs/windows-sandbox-rs/src/lib.rs
pub struct WindowsSandbox { perm: SandboxPermissions }

impl Sandbox for WindowsSandbox {
    fn wrap(&self, cmd: Command) -> Result<Command, SandboxError> {
        // 1. CreateRestrictedToken with disabled SIDs (everyone group sans
        //    user-write privs, etc.)
        // 2. CreateProcessAsUserW with the restricted token, preserving
        //    stdin/stdout/stderr handles.
        // 3. ACL the writable_roots to grant write perms to the new token.
        // …
    }
    fn name(&self) -> &str { "windows-restricted-token" }
}

// =============================================================================
// Comparison summary
//
//   Concept            | Upstream                                   | learn-codex (s08)
//   -------------------+--------------------------------------------+----------------------
//   trait              | trait Sandbox (Send + Sync)                | interface Sandbox
//   per-OS dispatch    | #[cfg(target_os)]                          | go:build tags
//   macOS impl         | full Seatbelt profile (mach-lookup etc.)   | minimal real Seatbelt
//   linux impl         | real Landlock + bwrap fallback             | stub w/ stderr warning
//   windows impl       | real CreateRestrictedToken                 | noop
//   permission shape   | rich struct (network allowlist, etc.)      | trimmed Permissions
// =============================================================================
