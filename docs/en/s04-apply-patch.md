---
title: "s04 · V4A patch DSL: parse + apply"
chapter: 4
slug: s04-apply-patch
est_read_min: 12
---

# s04 · V4A patch DSL: parse + apply

> What this teaches: codex's protocol-layer file edit format. Why not unified diff? How to parse it, how to locate the chunk exactly, how to write atomically.

---

## Problem

When the LLM wants to change a file, the naive approach is to ask it to emit the entire new file. That's wasteful for small edits and error-prone. Unified diff (`@@ -34,5 +34,7 @@`) is the second-most-natural choice — but LLMs frequently get the line numbers wrong, and the "fuzzy matching" most diff tools do just defers the error to runtime.

Codex's answer is V4A:

```
*** Begin Patch
*** Update File: pkg/foo/x.go
@@ optional anchor
 context line
-old line
+new line
*** End Patch
```

Each chunk is a run of lines prefixed by one of three markers: `+` is a new line, `-` is the old line to delete, leading-space is context (must match the file *exactly*). **No line numbers — purely text-anchored**, so the LLM can't fabricate fake line numbers.

## Solution

Two independent stages:

1. **Parse** — text → `[]Hunk`: `AddFile{Path,Contents}` / `DeleteFile{Path}` / `UpdateFile{Path, MovePath?, Chunks: []UpdateChunk{ChangeContext, OldLines, NewLines, IsEOF}}`. `Add` is a run of `+` lines; `Update` is one or more `@@ ...` chunks.
2. **Apply** — for each `Update`: read file, find an **exact contiguous match** of `OldLines` in the file, replace with `NewLines`, atomic write (temp + rename). `Add` writes a new file. `Delete` removes. `Move` renames after the update.

## How It Works

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

Core 25 lines from [`agents/s04-apply-patch/applier.go`](https://github.com/Ding-Ye/learn-codex/blob/main/agents/s04-apply-patch/applier.go):

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

**4 non-obvious points**:

1. **Context lines go into both OldLines and NewLines.** That way `findChunk(cur, OldLines)` aligns the context, and after replacement context is still where it was (it's also in NewLines).
2. **Chunks are order-sensitive.** Each chunk's replacement shifts subsequent line indices. Apply in source order; no dynamic re-numbering.
3. **Exact match, not fuzzy.** `findChunk` compares with `lines[i+j] != old[j]` — one character off and it fails. This is codex's core safety guarantee.
4. **Atomic writes.** `writeFile` writes to `<path>.tmp.learn-codex` then `os.Rename`. Even on a panic mid-write the original file isn't truncated.

## What Changed (vs. s03)

s03 added the `shell` tool; s04 adds the second tool, `apply_patch`. Both can coexist as independent Tool implementations.

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

## Try It

```bash
cd agents/s04-apply-patch

# Demo Add
cat <<'EOF' | go run ./cmd
*** Begin Patch
*** Add File: greet.txt
+hello
+world
*** End Patch
EOF
cat greet.txt   # → hello\nworld

# Demo Update
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

## Upstream Source Reading

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
    change_context: Option<String>,
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

**Reading notes**:

- **`Filesystem` trait.** Upstream abstracts the FS so tests can inject an in-memory FS. We use `os.*` directly.
- **`AppliedPatchDelta` return.** Upstream returns a structured "what was actually modified" record consumed by rollout/TUI diff view. We return `[]string`.
- **`is_end_of_file` field.** When a chunk runs to EOF, upstream avoids assuming a trailing newline — a common boundary case in real code. We carry the field but don't use it in the applier.
- **fsync.** Upstream fsyncs the temp file before rename so a crash doesn't lose the write. We don't.

**Read further**: start at `apply_patch::parse_patch`, follow `Hunk` into `apply_patch::apply_to_filesystem`, and finally see how `AppliedPatchDelta` is consumed by `core/src/codex_thread.rs::record_patch_applied`. That trace is the real-source map for s04 → s05 → s07.

---

**Next**: s05 puts a "user approval" gate in front of both shell and apply_patch — the `ApprovalPolicy` enum + `EvExecApprovalRequest` round-trip.
