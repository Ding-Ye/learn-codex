//go:build !darwin && !linux

package s08

import (
	"fmt"
	"os"
	"os/exec"
)

type noopSandbox struct{}

func platformSandbox() Sandbox { return noopSandbox{} }

func (noopSandbox) Name() string { return "noop" }

func (noopSandbox) Wrap(cmd *exec.Cmd, perm Permissions) (*exec.Cmd, error) {
	fmt.Fprintln(os.Stderr, "[learn-codex/s08] WARNING: no-op sandbox on this platform; running un-sandboxed.")
	return cmd, nil
}
