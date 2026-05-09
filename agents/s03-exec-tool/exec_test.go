package s03

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestShellRunsAndCapturesOutput(t *testing.T) {
	res := Run(context.Background(), ExecParams{
		Command: []string{"sh", "-c", "echo hello"},
	}, nil)
	if res.ExitCode != 0 {
		t.Errorf("exit=%d want 0; err=%v", res.ExitCode, res.Err)
	}
	if !strings.Contains(string(res.Stdout), "hello") {
		t.Errorf("stdout=%q missing 'hello'", string(res.Stdout))
	}
}

func TestShellNonZeroExit(t *testing.T) {
	res := Run(context.Background(), ExecParams{
		Command: []string{"sh", "-c", "exit 7"},
	}, nil)
	if res.ExitCode != 7 {
		t.Errorf("exit=%d want 7", res.ExitCode)
	}
}

func TestShellStreamsDeltasInOrder(t *testing.T) {
	deltas := make(chan OutputDelta, 16)
	done := make(chan ExecResult, 1)
	go func() {
		done <- Run(context.Background(), ExecParams{
			Command: []string{"sh", "-c", "echo A; echo B; echo C"},
		}, deltas)
		close(deltas)
	}()

	var out strings.Builder
	for d := range deltas {
		if d.Stream == StreamStdout {
			out.Write(d.Bytes)
		}
	}
	res := <-done
	if res.ExitCode != 0 {
		t.Fatalf("exit=%d", res.ExitCode)
	}
	if got := out.String(); got != "A\nB\nC\n" {
		t.Errorf("deltas concatenated=%q want \"A\\nB\\nC\\n\"", got)
	}
}

func TestShellCapsOutput(t *testing.T) {
	// Generate ~16 KB via shell yes(1)-style loop, cap at 1 KB.
	res := Run(context.Background(), ExecParams{
		Command:        []string{"sh", "-c", "i=0; while [ $i -lt 1024 ]; do echo XXXXXXXXXXXXXXXX; i=$((i+1)); done"},
		OutputCapBytes: 1024,
	}, nil)
	if !res.Truncated {
		t.Errorf("expected Truncated=true; got false")
	}
	if len(res.Stdout) > 1024 {
		t.Errorf("stdout=%d bytes, expected ≤ 1024", len(res.Stdout))
	}
	// process should still report success (we drained the rest)
	if res.ExitCode != 0 {
		t.Errorf("exit=%d want 0", res.ExitCode)
	}
}

func TestShellTimeoutKills(t *testing.T) {
	res := Run(context.Background(), ExecParams{
		Command: []string{"sh", "-c", "sleep 5"},
		Timeout: 100 * time.Millisecond,
	}, nil)
	if res.ExitCode == 0 {
		t.Errorf("expected non-zero exit on timeout")
	}
	if res.Err == nil {
		t.Errorf("expected ctx error in res.Err on timeout")
	}
}
