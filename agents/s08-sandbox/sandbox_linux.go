//go:build linux

package s08

import (
	"fmt"
	"os"
	"os/exec"
)

type linuxSandbox struct{}

func platformSandbox() Sandbox { return linuxSandbox{} }

func (linuxSandbox) Name() string { return "linux-stub" }

// Wrap on linux is a STUB.
//
// Real Codex uses the Landlock LSM (`landlock_create_ruleset(2)` /
// `landlock_add_rule(2)` / `landlock_restrict_self(2)`) and may also fall
// back to bubblewrap. Doing it for real in Go requires:
//
//   - golang.org/x/sys/unix Landlock helpers (Linux 5.13+)
//   - or shelling out to `bwrap` (which means depending on it being installed)
//
// learn-codex deliberately keeps a stub here so the chapter stays readable.
// To make it real, replace this file with one that calls landlock_* via
// golang.org/x/sys/unix; see upstream-readings/s08-sandbox.rs for the exact
// shape.
func (linuxSandbox) Wrap(cmd *exec.Cmd, perm Permissions) (*exec.Cmd, error) {
	fmt.Fprintln(os.Stderr, "[learn-codex/s08] WARNING: linux sandbox is a stub; running un-sandboxed.")
	fmt.Fprintln(os.Stderr, "[learn-codex/s08] perm:", perm)
	return cmd, nil
}
