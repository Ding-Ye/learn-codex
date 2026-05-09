---
title: "s08 · sandbox: Seatbelt + Landlock adapters"
chapter: 8
slug: s08-sandbox
est_read_min: 11
---

# s08 · sandbox: Seatbelt + Landlock adapters

> What this teaches: another defence layer between s05's "ask-the-user" and s03's "actually run" — kernel-level isolation. Even if the user said yes, the command can only act inside the cage we drew.

---

## Problem

s05's approval is a **human** layer of defence. s06's execpolicy is a **policy** layer. After both succeed, the command really runs. But the LLM can hand us something innocuous-looking that takes a sneaky route — `find . -name '*.go' -exec rm {} \;`. By the time you read `rm`, it's too late.

We need a final **kernel** layer: even if the program *wants* to delete `~/code`, the OS returns `EPERM`.

OSes give wildly different tools:

| OS | Tool | How |
|---|---|---|
| macOS | Seatbelt | `sandbox-exec -f profile.sb <cmd>`; the profile is an s-expression DSL |
| Linux | Landlock LSM | direct syscalls (5.13+) or wrap with bubblewrap |
| Windows | Restricted Token | `CreateRestrictedToken` + `CreateProcessAsUser` |

s08's job: make the agent code **completely OS-agnostic**. Upper layers just call `sandbox.Wrap(cmd, Permissions{...})` and get a runnable `*exec.Cmd`.

## Solution

A `Sandbox` interface plus three build-tag-selected impls:

```go
type Sandbox interface {
    Wrap(cmd *exec.Cmd, perm Permissions) (*exec.Cmd, error)
    Name() string
}
```

- **darwin** (real, runnable) — generate a Seatbelt profile, drop it in `/tmp/learn-codex-*.sb`, return `exec.Command("sandbox-exec", "-f", profile, ...)`.
- **linux** (teaching stub) — log a stderr warning, return the original cmd. The comment **clearly points** at upstream's real Landlock implementation.
- **windows / others** (noop) — same idea.

Why a deliberate stub on Linux? Real Landlock requires:
1. depending on `golang.org/x/sys/unix`;
2. detecting kernel ≥ 5.13;
3. ~80 LOC of syscall wrapping.

These would turn "the sandbox chapter" into "the Linux Landlock chapter" and obscure the lesson. We leave a **clearly flagged extension exercise**: upstream's Rust impl lives in `codex-rs/linux-sandbox/src/landlock.rs`, ~200 LOC of syscall wrapping; translate it line-by-line if you need it.

## How It Works

```
Codex.runShellTool(params):
   cmd := exec.Command(params.Command[0], params.Command[1:]...)
                                        │
                                        ▼
   wrapped, _ := sandbox.New().Wrap(cmd, Permissions{
       ReadOnly:      false,
       WritableRoots: []string{params.Cwd},     // writable only inside cwd
       AllowNetwork:  false,
   })
                                        │
                                        ▼
                       darwin:   builds seatbelt profile:
                                   (version 1)
                                   (deny default)
                                   (allow process-fork)
                                   (allow process-exec)
                                   (allow file-read*)
                                   (allow file-write* (subpath "/path/to/cwd"))
                                   (allow network* (remote ip "localhost:*"))
                                 returns: sandbox-exec -f /tmp/...sb <cmd>

                       linux:    [stub] returns cmd unchanged + stderr warning

                       other:    [noop] returns cmd unchanged + stderr warning
```

Core 25 lines from [`agents/s08-sandbox/sandbox_darwin.go`](https://github.com/Ding-Ye/learn-codex/blob/main/agents/s08-sandbox/sandbox_darwin.go):

```go
func buildSeatbeltProfile(perm Permissions) string {
    var b strings.Builder
    b.WriteString("(version 1)\n(deny default)\n")
    b.WriteString("(allow process-fork)\n(allow process-exec)\n")
    b.WriteString("(allow signal (target self))\n(allow file-read*)\n")
    if !perm.ReadOnly {
        for _, root := range perm.WritableRoots {
            abs, _ := filepath.Abs(root)
            fmt.Fprintf(&b, "(allow file-write* (subpath %q))\n", abs)
        }
    }
    if perm.AllowNetwork {
        b.WriteString("(allow network*)\n")
    } else {
        b.WriteString("(allow network* (remote ip \"localhost:*\"))\n")
    }
    return b.String()
}

func (darwinSandbox) Wrap(cmd *exec.Cmd, perm Permissions) (*exec.Cmd, error) {
    profile := buildSeatbeltProfile(perm)
    tmp, _ := os.CreateTemp("", "learn-codex-*.sb")
    tmp.WriteString(profile); tmp.Close()
    args := append([]string{"-f", tmp.Name(), cmd.Path}, cmd.Args[1:]...)
    return exec.Command("sandbox-exec", args...), nil
}
```

**4 non-obvious points**:

1. **`(deny default)` must come first.** Seatbelt is "deny then allow". Without it, you've allowed everything.
2. **Loopback must stay open.** Many commands hit 127.0.0.1 for name resolution / DNS proxy / TLS cache. A blanket no-network rule causes harmless commands to fail mysteriously.
3. **Build tags pick the impl.** No runtime branching — `sandbox_darwin.go` / `sandbox_linux.go` / `sandbox_other.go` each define `platformSandbox()` and only one compiles on each GOOS.
4. **Don't mutate the original cmd.** `Wrap` returns a *new* `*exec.Cmd`; the caller can still hold the unwrapped version for retries.

## What Changed (vs. s07)

```diff
+ type Permissions struct {
+     ReadOnly bool
+     WritableRoots []string
+     AllowNetwork bool
+ }
+ type Sandbox interface {
+     Wrap(cmd *exec.Cmd, perm Permissions) (*exec.Cmd, error)
+     Name() string
+ }
+ func New() Sandbox  // build-tag chooses darwin/linux/other

  // s03's Run() can now do:
  cmd := exec.CommandContext(ctx, p.Command[0], p.Command[1:]...)
+ wrapped, err := sandbox.New().Wrap(cmd, perm)
  // ... start wrapped, pump stdout/stderr, etc.
```

## Try It

```bash
cd agents/s08-sandbox

# (macOS) — sandbox actually blocks
go run ./cmd -ro -- sh -c 'echo nope > /etc/learn-codex-test'
# Operation not permitted

# Open /tmp for writes
go run ./cmd -ro -root /tmp -- sh -c 'echo ok > /tmp/learn-codex-test && cat /tmp/learn-codex-test'
# ok

# (linux/windows) you'll see [WARNING ... un-sandboxed] then it just runs
go test -v ./...
```

## Upstream Source Reading

```upstream:codex-rs/linux-sandbox/src/landlock.rs
// Source: codex-rs/linux-sandbox/src/landlock.rs (excerpt)

pub struct LandlockSandbox {
    abi: LandlockAbi,                // detected at startup
    writable_paths: Vec<PathBuf>,
    readable_paths: Vec<PathBuf>,
    allow_network: bool,
}

impl LandlockSandbox {
    pub fn restrict_self(&self) -> Result<(), LandlockError> {
        // 1. landlock_create_ruleset(&attr, sizeof(attr), 0) → ruleset_fd
        let ruleset_fd = unsafe {
            libc::syscall(SYS_LANDLOCK_CREATE_RULESET,
                          &attr, mem::size_of::<landlock_ruleset_attr>(), 0)
        };
        // 2. for each path: landlock_add_rule(ruleset_fd, RULE_PATH_BENEATH,
        //                                     &path_attr, 0)
        for path in &self.writable_paths {
            let fd = open_path(path)?;
            let attr = landlock_path_beneath_attr {
                allowed_access: LANDLOCK_ACCESS_FS_WRITE_FILE
                              | LANDLOCK_ACCESS_FS_REMOVE_FILE
                              | /* … */,
                parent_fd: fd,
            };
            unsafe { libc::syscall(SYS_LANDLOCK_ADD_RULE, ruleset_fd,
                                   LANDLOCK_RULE_PATH_BENEATH, &attr, 0); }
        }
        // 3. prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0)
        unsafe { libc::prctl(libc::PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); }
        // 4. landlock_restrict_self(ruleset_fd, 0)
        unsafe { libc::syscall(SYS_LANDLOCK_RESTRICT_SELF, ruleset_fd, 0); }
        Ok(())
    }
}
```

```upstream:codex-rs/sandboxing/src/seatbelt.rs
// Source: codex-rs/sandboxing/src/seatbelt.rs (excerpt)

pub struct SeatbeltSandbox { perm: SandboxPermissions }

impl SeatbeltSandbox {
    pub fn wrap(&self, cmd: Command) -> Command {
        let profile = self.build_profile();
        let path = write_temp_profile(&profile)?;
        let mut wrapped = Command::new("/usr/bin/sandbox-exec");
        wrapped.arg("-f").arg(path);
        wrapped.arg(cmd.get_program());
        wrapped.args(cmd.get_args());
        wrapped
    }

    fn build_profile(&self) -> String {
        // Same shape as learn-codex's buildSeatbeltProfile() but with more
        // exhaustive rules: subprocess limits, IPC restrictions, mach-lookup
        // allowlist (otherwise Apple-internal services like cfprefsd break).
    }
}
```

**Reading notes**:

- **Per-thread, not per-process.** Landlock affects only the thread that called `landlock_restrict_self`. Codex calls it inside the child process *after* spawn, so the parent isn't affected.
- **`mach-lookup` is the macOS landmine.** Upstream's Seatbelt profile has many more `(allow mach-lookup ...)` rules; without them, even cfprefsd or mDNSResponder calls fail and the command crashes via SIP. We dodge it by running simple commands.
- **`allow_network` defaults to off.** Upstream's shell tool defaults to "no network" because "the LLM curls something" is the most common foot-gun. We do the same.
- **`PR_SET_NO_NEW_PRIVS` must come first.** Linux's safety model demands "before dropping privileges, ensure you can't gain new ones" — otherwise setuid binaries can sidestep the cage.
- **Error UX.** Upstream rewrites stderr "Operation not permitted" into "this was blocked by the sandbox; relax `sandbox_mode` to allow"-style messages. We don't — that polish goes in s10's CLI driver.

**Read further**: start at `codex-rs/sandboxing/src/lib.rs::SandboxManager::get_platform_sandbox()`, follow the trait into the per-OS impls, then come back to `core/src/exec.rs::process_exec_tool_call` to see how sandbox + exec + delta tx all string together. That's the s03 → s08 → s10 trace.

---

**Next**: s09 wires in MCP (Model Context Protocol) — let external processes register their own tools, which the agent exposes alongside its built-ins.
