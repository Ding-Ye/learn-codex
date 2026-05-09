//go:build darwin

package s08

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type darwinSandbox struct{}

func platformSandbox() Sandbox { return darwinSandbox{} }

func (darwinSandbox) Name() string { return "darwin" }

// Wrap generates a Seatbelt profile in a tmpfile and returns a new
// exec.Cmd that runs `sandbox-exec -f <profile> <cmd>...`.
//
// `sandbox-exec` is shipped in /usr/bin on every macOS we care about (10.5+).
// The Seatbelt profile language is "Apple-specific s-expressions" — see
// `man sandbox-exec` and `/System/Library/Sandbox/Profiles/*.sb`.
func (darwinSandbox) Wrap(cmd *exec.Cmd, perm Permissions) (*exec.Cmd, error) {
	profile := buildSeatbeltProfile(perm)
	tmp, err := os.CreateTemp("", "learn-codex-*.sb")
	if err != nil {
		return nil, fmt.Errorf("create profile tmpfile: %w", err)
	}
	if _, err := tmp.WriteString(profile); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("write profile: %w", err)
	}
	_ = tmp.Close()

	args := append([]string{"-f", tmp.Name(), cmd.Path}, cmd.Args[1:]...)
	wrapped := exec.Command("sandbox-exec", args...)
	wrapped.Dir = cmd.Dir
	wrapped.Env = cmd.Env
	wrapped.Stdin = cmd.Stdin
	wrapped.Stdout = cmd.Stdout
	wrapped.Stderr = cmd.Stderr
	return wrapped, nil
}

func buildSeatbeltProfile(perm Permissions) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n")
	// always allow process startup essentials
	b.WriteString("(allow process-fork)\n")
	b.WriteString("(allow process-exec)\n")
	b.WriteString("(allow signal (target self))\n")
	// reading is essentially always required
	b.WriteString("(allow file-read*)\n")

	if !perm.ReadOnly {
		for _, root := range perm.WritableRoots {
			abs, _ := filepath.Abs(root)
			fmt.Fprintf(&b, "(allow file-write* (subpath %q))\n", abs)
		}
	}
	if perm.AllowNetwork {
		b.WriteString("(allow network*)\n")
	} else {
		// loopback always allowed (otherwise even getaddrinfo fails locally)
		b.WriteString("(allow network* (remote ip \"localhost:*\"))\n")
	}
	return b.String()
}
