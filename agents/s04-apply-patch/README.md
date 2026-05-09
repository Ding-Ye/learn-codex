# s04 · apply-patch (V4A DSL)

Codex's text-based patch DSL — explicit, unambiguous, no fuzzy matching:

```
*** Begin Patch
*** Update File: pkg/foo/x.go
@@ optional anchor
 context line
-old line
+new line
*** End Patch
```

Plus `*** Add File:`, `*** Delete File:`, `*** Move to:`. This session ships:

- `Hunk` / `UpdateChunk` types (patch.go).
- `Parse(text) (*Patch, error)` (parser.go) — recursive-descent over lines.
- `Apply(root, *Patch) (changed []string, error)` (applier.go) — exact-match find + replace, atomic per-file write.

## Run

```bash
cd agents/s04-apply-patch
cat <<EOF | go run ./cmd
*** Begin Patch
*** Add File: greet.txt
+hello
+world
*** End Patch
EOF
# → greet.txt printed; file written to cwd

go test -v ./...
```

## Upstream

- [`codex-rs/apply-patch/src/lib.rs`](https://github.com/openai/codex/blob/main/codex-rs/apply-patch/src/lib.rs).
- [`upstream-readings/s04-apply-patch.rs`](../../upstream-readings/s04-apply-patch.rs).
