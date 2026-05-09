# s08 · sandbox adapters (Seatbelt + Landlock)

Per-platform OS-level isolation for s03's exec tool.

- `Sandbox.Wrap(cmd, Permissions) → *exec.Cmd` — returns a new Cmd that runs the original under sandbox.
- darwin → real `sandbox-exec` with a generated Seatbelt profile.
- linux → stub (Landlock is the right answer; left as a clearly-documented exercise so the chapter stays in 200 LOC).
- windows / others → no-op with a stderr warning.

## Run

```bash
cd agents/s08-sandbox

# (macOS) read-only sandbox blocks file writes outside /tmp:
go run ./cmd -ro -- sh -c 'echo blocked > /etc/learn-codex-test'   # → operation not permitted
go run ./cmd -ro -root /tmp -- sh -c 'echo ok > /tmp/learn-codex-test'

go test -v ./...
```

## Upstream

- [`codex-rs/sandboxing/src/lib.rs`](https://github.com/openai/codex/blob/main/codex-rs/sandboxing) — platform abstraction.
- [`codex-rs/linux-sandbox/`](https://github.com/openai/codex/tree/main/codex-rs/linux-sandbox) — real Landlock.
- [`upstream-readings/s08-sandbox.rs`](../../upstream-readings/s08-sandbox.rs).
