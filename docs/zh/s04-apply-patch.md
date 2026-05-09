---
title: "s04 · V4A patch DSL：解析 + 应用"
chapter: 4
slug: s04-apply-patch
est_read_min: 12
---

# s04 · V4A patch DSL：解析 + 应用

> 教什么：codex 编辑文件的协议层。为什么不是 unified diff？怎么解析、怎么精确定位、怎么原子写入。

---

## Problem / 问题

LLM 要改文件，最朴素的做法是让它输出整段新文件，但小改动也得吐千行——浪费上下文、易出错。第二种做法是 unified diff（`@@ -34,5 +34,7 @@`）——但 LLM 经常把行号搞错；diff 工具的"模糊匹配"只是把错误推迟到运行时。

codex 的解决方案是 V4A：

```
*** Begin Patch
*** Update File: pkg/foo/x.go
@@ optional anchor
 context line
-old line
+new line
*** End Patch
```

每个 chunk 用 `+/-/(空格)` 三类前缀的连续行表达："+" 是新行，"-" 是要删的旧行，空格开头是 context（必须和文件里精确一致）。**没有行号，全靠精确文本匹配**——LLM 编不出来虚假行号。

## Solution / 解决方案

两个独立步骤：

1. **Parse**——把 patch 文本读成 `[]Hunk`：`AddFile{Path,Contents}` / `DeleteFile{Path}` / `UpdateFile{Path, MovePath?, Chunks: []UpdateChunk{ChangeContext, OldLines, NewLines, IsEOF}}`。`Add` 用一连串 `+` 行；`Update` 是一个或多个 `@@ ...` chunk。
2. **Apply**——对 `Update`：读文件、按 `OldLines` 在文件里找 **精确连续匹配**、替换成 `NewLines`、原子写回（write-temp-then-rename）。`Add` 直接写新文件。`Delete` 直接删。`Move` 在 `Update` 之后改名。

## How It Works / 工作原理

```
patch text                    Parser                      *Patch
─────────────────             ──────                      ──────
*** Begin Patch ───────▶  scan header
*** Update File: x.go  ───▶  push UpdateFile{Path:"x.go"}
@@                       ─▶  start chunk
 a                       ─▶  context  → OldLines += "a", NewLines += "a"
-b                       ─▶  delete   → OldLines += "b"
+B                       ─▶  insert   → NewLines += "B"
*** End Patch  ────────▶  done


Apply(root, patch)
  for each Hunk:
    case AddFile:    write root/path = contents
    case DeleteFile: os.Remove root/path
    case UpdateFile: 
       cur := splitLines(read root/path)
       for each chunk:
         idx := findChunk(cur, chunk.OldLines)         ← exact match
         if idx < 0: ERROR(not found)
         cur = cur[:idx] + chunk.NewLines + cur[idx+len(OldLines):]
       atomic write joinLines(cur)
       if MovePath: rename
```

核心 25 行（节选自 [`agents/s04-apply-patch/applier.go`](https://github.com/Ding-Ye/learn-codex/blob/main/agents/s04-apply-patch/applier.go)）：

```go
func applyChunks(before string, chunks []UpdateChunk) (string, error) {
    cur := strings.Split(before, "\n")
    for ci, ch := range chunks {
        idx := findChunk(cur, ch.OldLines)
        if idx < 0 {
            return "", fmt.Errorf("chunk %d: old lines not found (anchor=%q)", ci, ch.ChangeContext)
        }
        cur = append(append(append([]string{}, cur[:idx]...),
                            ch.NewLines...),
                     cur[idx+len(ch.OldLines):]...)
    }
    return strings.Join(cur, "\n"), nil
}

func findChunk(lines, old []string) int {
    if len(old) == 0 { return 0 }
outer:
    for i := 0; i+len(old) <= len(lines); i++ {
        for j := range old { if lines[i+j] != old[j] { continue outer } }
        return i
    }
    return -1
}
```

**4 个非显然之处**：

1. **context 行同时进 OldLines 和 NewLines**——这样 `findChunk` 用 `OldLines` 定位时把 context 也对齐进来，替换之后 context 还在原位（因为它也在 `NewLines` 里）。
2. **chunks 顺序敏感**——上一 chunk 的替换会改变下一 chunk 的行号。我们按文件出现顺序应用，不需要任何"动态修正"。
3. **精确匹配，不模糊**——`findChunk` 用 `lines[i+j] != old[j]` 比较，一字之差就 fail。这是 codex 的核心安全保证。
4. **原子写入**——`writeFile` 先写 `<path>.tmp.learn-codex` 再 `os.Rename`。即使过程中 panic，原文件也不会被截断。

## What Changed / 与 s03 的变化

s03 加了 `shell` 工具；s04 加了第二个工具 `apply_patch`。两个都是独立 Tool 实现，可以并存。

```diff
+ type Hunk interface{ isHunk() }
+ type AddFile struct { Path, Contents string }
+ type DeleteFile struct { Path string }
+ type UpdateFile struct { Path, MovePath string; Chunks []UpdateChunk }
+ type UpdateChunk struct { ChangeContext string; OldLines, NewLines []string; IsEOF bool }
+
+ func Parse(text string) (*Patch, error)
+ func Apply(root string, p *Patch) (changed []string, err error)
```

## Try It / 动手试一试

```bash
cd agents/s04-apply-patch

# 演示 Add
cat <<'EOF' | go run ./cmd
*** Begin Patch
*** Add File: greet.txt
+hello
+world
*** End Patch
EOF
cat greet.txt   # → hello\nworld

# 演示 Update
echo -e 'a\nb\nc' > x.txt
cat <<'EOF' | go run ./cmd
*** Begin Patch
*** Update File: x.txt
@@
 a
-b
+B
 c
*** End Patch
EOF
cat x.txt   # → a\nB\nc

go test -v ./...
```

## Upstream Source Reading / 上游源码阅读

```upstream:codex-rs/apply-patch/src/lib.rs
// Source: codex-rs/apply-patch/src/lib.rs

pub enum Hunk {
    AddFile { path: PathBuf, contents: String },
    DeleteFile { path: PathBuf },
    UpdateFile {
        path: PathBuf,
        move_path: Option<PathBuf>,
        chunks: Vec<UpdateFileChunk>,
    },
}

pub struct UpdateFileChunk {
    old_lines: Vec<String>,
    new_lines: Vec<String>,
    change_context: Option<String>,    // the "@@ ..." anchor, optional
    is_end_of_file: bool,
}

pub fn parse_patch(text: &str) -> Result<Vec<Hunk>, ParseError> { /* … */ }

pub fn apply_patch(
    cwd: &Path,
    hunks: Vec<Hunk>,
    fs: &mut impl Filesystem,
) -> Result<AppliedPatchDelta, ApplyError> {
    // 1. for each hunk, read file (Update) or skip (Add/Delete)
    // 2. apply chunks in order; each must match exactly
    // 3. write atomically (write to temp, fsync, rename)
    // 4. return AppliedPatchDelta {created, deleted, modified} for the
    //    rollout recorder
}
```

**对照阅读要点**：

- **`Filesystem` trait**：上游把文件系统抽出来一个 trait，便于测试时注入 in-memory fs。学习版直接用 `os.*`。
- **`AppliedPatchDelta` 返回值**：上游把"实际改了哪些文件"以结构化形式返给调用方，被 rollout / TUI diff view 消费。学习版只返回 `[]string`。
- **`is_end_of_file` 字段**：当 chunk 是文件末尾时，上游避免假设末尾有 newline——这是真实代码里很常见的边界 case。学习版预留了字段但没在 applier 里用。
- **fsync**：上游写完 temp 文件会显式 fsync 再 rename，确保崩溃时不丢。学习版省。

**想读更多**：从 `apply_patch::parse_patch` 入手，跟着 `Hunk` 进 `apply_patch::apply_to_filesystem`，最后看 `AppliedPatchDelta` 在 `core/src/codex_thread.rs` 的 `record_patch_applied` 怎么被消费——这是 s04 → s05 → s07 的真实代码地图。

---

**下一节预告**：s05 给 shell 和 apply_patch 都装上"用户审批"前置门——`ApprovalPolicy` + `EvExecApprovalRequest` 往返。
