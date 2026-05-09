# s06 · execpolicy DSL

A tiny line-oriented rules format that powers s05's `Trusted` callback.

```
default prompt
rule allow  prefix "git status"
rule prompt prefix "rm"  reason "destructive"
rule forbid prefix "sudo" reason "privilege escalation"
rule allow  exact  "go test ./..."
```

`Parse(io.Reader) → *Policy`. `Policy.Classify(command []string) → (Decision, *Rule)`. First-match wins; no match → `Default`.

## Run

```bash
cd agents/s06-execpolicy
go run ./cmd testdata/default.rules -- git status
go run ./cmd testdata/default.rules -- rm -rf /tmp
go run ./cmd testdata/default.rules -- sudo whatever
go test -v ./...
```

## Upstream

- [`codex-rs/core/src/exec_policy.rs`](https://github.com/openai/codex/blob/main/codex-rs/core/src/exec_policy.rs) — the integration with the agent loop.
- [`codex-rs/execpolicy/src/`](https://github.com/openai/codex/tree/main/codex-rs/execpolicy) — the full Starlark-based policy crate.
- [`upstream-readings/s06-execpolicy.rs`](../../upstream-readings/s06-execpolicy.rs).
