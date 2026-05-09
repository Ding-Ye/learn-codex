package s08

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestNewReturnsAnImpl(t *testing.T) {
	s := New()
	if s == nil {
		t.Fatal("New returned nil")
	}
	switch runtime.GOOS {
	case "darwin":
		if s.Name() != "darwin" {
			t.Errorf("name=%q want darwin", s.Name())
		}
	case "linux":
		if s.Name() != "linux-stub" {
			t.Errorf("name=%q want linux-stub", s.Name())
		}
	default:
		if s.Name() != "noop" {
			t.Errorf("name=%q want noop", s.Name())
		}
	}
}

func TestWrapDoesNotMutateOriginal(t *testing.T) {
	s := New()
	cmd := exec.Command("echo", "x")
	origPath := cmd.Path
	origArgs := append([]string(nil), cmd.Args...)
	wrapped, err := s.Wrap(cmd, Permissions{ReadOnly: true})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if cmd.Path != origPath {
		t.Errorf("original Path mutated: %q → %q", origPath, cmd.Path)
	}
	if len(cmd.Args) != len(origArgs) {
		t.Errorf("original Args mutated: %v → %v", origArgs, cmd.Args)
	}
	_ = wrapped
}

func TestSeatbeltProfileAlwaysDeniesByDefault(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("seatbelt profile is darwin-only")
	}
	prof := buildSeatbeltProfile(Permissions{ReadOnly: false, WritableRoots: []string{"/tmp"}, AllowNetwork: true})
	if !strings.Contains(prof, "(deny default)") {
		t.Errorf("missing deny-default: %q", prof)
	}
	if !strings.Contains(prof, "(allow file-read*)") {
		t.Errorf("missing allow file-read: %q", prof)
	}
	if !strings.Contains(prof, "/tmp") {
		t.Errorf("writable root not present: %q", prof)
	}
	if !strings.Contains(prof, "(allow network*)") {
		t.Errorf("network not allowed: %q", prof)
	}

	prof2 := buildSeatbeltProfile(Permissions{ReadOnly: true, AllowNetwork: false})
	if strings.Contains(prof2, "file-write") {
		t.Errorf("unexpected write rule in read-only profile: %q", prof2)
	}
	if !strings.Contains(prof2, "localhost") {
		t.Errorf("loopback not allowed in network-off profile: %q", prof2)
	}
}

func TestDarwinSandboxRunsCommand(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires darwin sandbox-exec")
	}
	s := New()
	wrapped, err := s.Wrap(exec.Command("sh", "-c", "echo HI"), Permissions{ReadOnly: true, AllowNetwork: false})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	out, err := wrapped.CombinedOutput()
	if err != nil {
		// sandbox-exec prints the warning that it's deprecated — but the
		// command should still run. Only fail if no "HI" came out.
		if !strings.Contains(string(out), "HI") {
			t.Fatalf("err=%v out=%q", err, string(out))
		}
	}
	if !strings.Contains(string(out), "HI") {
		t.Errorf("output missing HI: %q", string(out))
	}
}
