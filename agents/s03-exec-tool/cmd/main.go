package main

import (
	"context"
	"fmt"
	"os"

	s03 "github.com/Ding-Ye/learn-codex/agents/s03-exec-tool"
)

// `go run ./cmd -- echo hello` runs and shows streaming + the final summary.
func main() {
	args := os.Args[1:]
	if len(args) >= 1 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Println("usage: go run ./cmd -- <command...>")
		os.Exit(2)
	}
	deltas := make(chan s03.OutputDelta, 64)
	done := make(chan s03.ExecResult, 1)
	go func() {
		done <- s03.Run(context.Background(), s03.ExecParams{Command: args}, deltas)
		close(deltas)
	}()
	for d := range deltas {
		w := os.Stdout
		if d.Stream == s03.StreamStderr {
			w = os.Stderr
		}
		_, _ = w.Write(d.Bytes)
	}
	res := <-done
	fmt.Fprintf(os.Stderr, "\n  [exit=%d truncated=%v err=%v]\n", res.ExitCode, res.Truncated, res.Err)
}
