package s08

import "os/exec"

// Permissions describes what the sandbox should permit. Mirror of the
// upstream `SandboxPermissions` struct but with only the fields that fit
// into a teaching example.
type Permissions struct {
	// ReadOnly: the child can read everything but write nothing.
	ReadOnly bool
	// WritableRoots: directories the child may write under (when ReadOnly is false).
	WritableRoots []string
	// AllowNetwork: outbound network on/off.
	AllowNetwork bool
}

// Sandbox is the per-platform adapter contract. Wrap returns a *new* exec.Cmd
// that, when started, runs `cmd` inside the sandbox. The original cmd is not
// modified.
//
// learn-codex provides three implementations (compile-time picked):
//
//   - darwinSandbox  — real: generates a Seatbelt profile, prepends `sandbox-exec -p ...`
//   - linuxSandbox   — stub: logs a clear "Landlock not implemented in learn-codex" line
//   - noopSandbox    — windows + fallback: runs without isolation
type Sandbox interface {
	Wrap(cmd *exec.Cmd, perm Permissions) (*exec.Cmd, error)
	// Name returns "darwin"/"linux-stub"/"noop".
	Name() string
}

// New picks the right Sandbox for the current GOOS at compile time.
// (See sandbox_darwin.go / sandbox_linux.go / sandbox_other.go.)
func New() Sandbox { return platformSandbox() }
