---
title: "s08 · sandbox：Seatbelt + Landlock 适配"
chapter: 8
slug: s08-sandbox
est_read_min: 11
---

# s08 · sandbox：Seatbelt + Landlock 适配

> 教什么：在 s05 的"问问看"和 s03 的"真的跑"之间再插一层——OS 内核级隔离。即便用户点了"是"，命令也只能在我们划好的笼子里行动。

---

## Problem / 问题

s05 的 approval 是**人**这一层的防线，s06 的 execpolicy 是**策略**这一层的防线。两道都过了，命令总会真的跑。但 LLM 可能给我们一个看起来人畜无害、其实绕路的命令——比如 `find . -name '*.go' -exec rm {} \;`。等你看到 `rm` 时已经太晚。

我们需要**内核**这一层的最后防线：即便程序"想"删 `~/code`，OS 直接 `EPERM`。

不同 OS 给的工具完全不一样：

| OS | 工具 | 怎么用 |
|---|---|---|
| macOS | Seatbelt | `sandbox-exec -f profile.sb <cmd>`，profile 是 s-exp DSL |
| Linux | Landlock LSM | 直接 syscall（5.13+），或者 bubblewrap 包一层 |
| Windows | Restricted Token | `CreateRestrictedToken` + `CreateProcessAsUser` |

s08 的目标是**让 agent 的代码完全不感知** OS 差异：上层只调 `sandbox.Wrap(cmd, Permissions{...})`，得到一个能跑的 `*exec.Cmd`。

## Solution / 解决方案

`Sandbox` interface（接口）+ 三个 build-tag 选择的实现：

```go
type Sandbox interface {
    Wrap(cmd *exec.Cmd, perm Permissions) (*exec.Cmd, error)
    Name() string
}
```

- **darwin**（真实可运行）—— 生成一份 Seatbelt profile，落到 `/tmp/learn-codex-*.sb`，返回 `exec.Command("sandbox-exec", "-f", profile, ...)`。
- **linux**（教学用 stub）—— 打一行 stderr 警告，原 cmd 直接返回。**注释里写清楚**真实实现需要 `golang.org/x/sys/unix` 的 Landlock helper。
- **windows / 其他**（noop）—— 同 stub。

为什么 linux 故意留 stub？因为 Landlock 真实实现需要：
1. 引入 `golang.org/x/sys/unix` 依赖；
2. 检测 kernel ≥ 5.13；
3. 写 ~80 行 syscall 包装。

这些会让"sandbox 章节"变成"Linux 内核 Landlock 章节"，淹没教学重点。我们留一个**清晰标明的扩展练习**：上游 Rust 实现就在 `codex-rs/linux-sandbox/src/landlock.rs`，~ 200 行 syscall 包装；想做就一一对应翻译过去。

## How It Works / 工作原理

```
Codex.runShellTool(params):
   cmd := exec.Command(params.Command[0], params.Command[1:]...)
                                        │
                                        ▼
   wrapped, _ := sandbox.New().Wrap(cmd, Permissions{
       ReadOnly:      false,
       WritableRoots: []string{params.Cwd},     // 只在 cwd 可写
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

核心 25 行（节选自 [`agents/s08-sandbox/sandbox_darwin.go`](https://github.com/Ding-Ye/learn-codex/blob/main/agents/s08-sandbox/sandbox_darwin.go)）：

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

**4 个非显然之处**：

1. **deny default 必须最前**——Seatbelt 是"先 deny，再 allow"模式。如果你不写 `(deny default)`，等于完全打开。
2. **localhost 必须留口**——很多命令做名字解析会戳 127.0.0.1（resolver / dns proxy / TLS 校验缓存）。完全断网会让无害命令神秘失败。
3. **build tag 选实现**——不需要 runtime branch；`sandbox_darwin.go` / `sandbox_linux.go` / `sandbox_other.go` 三份各自的 `platformSandbox()` 只在对应 GOOS 编译。这是 Go 标准做法。
4. **不修改原 cmd**——Wrap 返回**新** `*exec.Cmd`，不动入参。这样调用方 retry 时还能拿到 unwrapped 的备份。

## What Changed / 与 s07 的变化

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

## Try It / 动手试一试

```bash
cd agents/s08-sandbox

# (macOS) — sandbox 真的拦
go run ./cmd -ro -- sh -c 'echo nope > /etc/learn-codex-test'
# Operation not permitted

# 给 /tmp 开放写
go run ./cmd -ro -root /tmp -- sh -c 'echo ok > /tmp/learn-codex-test && cat /tmp/learn-codex-test'
# ok

# (linux/windows) 会看到 [WARNING ... un-sandboxed] 然后照常跑
go test -v ./...
```

## Upstream Source Reading / 上游源码阅读

```upstream:codex-rs/linux-sandbox/src/landlock.rs
// Source: codex-rs/linux-sandbox/src/landlock.rs (节选)

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
// Source: codex-rs/sandboxing/src/seatbelt.rs (节选)

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

**对照阅读要点**：

- **per-thread vs per-process**：Landlock 是**线程级**——只对调用 `landlock_restrict_self` 的那个线程生效。codex 在 child process spawn **之后**让 child 自己调，避免影响父进程。
- **`mach-lookup` 是 macOS 隐藏地雷**：上游 Seatbelt profile 比我们多很多 `(allow mach-lookup ...)` 行，否则连 `cfprefsd` / `mDNSResponder` 都连不上，命令会因为 SIP 报错挂掉。学习版只跑简单命令所以省了。
- **`allow_network` 默认关**：上游对 shell tool 默认拒绝出网，因为 LLM "顺手 curl" 的动作出问题最频繁。我们也是同样默认。
- **`PR_SET_NO_NEW_PRIVS` 必须先调**：Linux 的安全模型要求"在禁掉权限之前，先确保没办法获得新权限"，否则 `setuid` 程序可以绕过。
- **错误回滤到用户**：上游会在 stderr 看到 `Operation not permitted` 之类时改写成"this command was blocked by the sandbox; relax `sandbox_mode` to allow"——这个 UX 工作我们没做，是 s10 CLI driver 时可以补的细节。

**想读更多**：从 `codex-rs/sandboxing/src/lib.rs::SandboxManager::get_platform_sandbox()` 入手，跟着 trait 进各 OS 的实现，再回头看 `core/src/exec.rs` 的 `process_exec_tool_call` 怎么把 sandbox + exec + delta tx 串起来。这条线是 s03 → s08 → s10 的代码地图。

---

**下一节预告**：s09 给 agent 加上 MCP（Model Context Protocol）桥——让外部进程注册自己的工具，agent 把它们和内置工具一起暴露给 LLM。
