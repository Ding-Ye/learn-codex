package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	s05 "github.com/Ding-Ye/learn-codex/agents/s05-approval"
)

// Tiny demo: simulate a model emitting two tool calls. Console asks the user
// to approve each one. Demonstrates that the gate emits and blocks correctly.
func main() {
	g := s05.NewGate(s05.ApprovalOnRequest)
	emit := func(r s05.Request) {
		fmt.Printf("\n[approve?] command: %v reason=%q\n", r.Command, r.Reason)
	}

	commands := [][]string{
		{"ls", "/tmp"},
		{"rm", "-rf", "/important"},
	}

	stdin := bufio.NewReader(os.Stdin)
	for i, cmd := range commands {
		req := s05.Request{ID: fmt.Sprintf("call-%d", i), Command: cmd}
		decisionCh := make(chan s05.Decision, 1)
		go func() {
			decisionCh <- g.Check(context.Background(), req, emit)
		}()
		fmt.Print("y/n > ")
		ans, _ := stdin.ReadString('\n')
		_ = g.Resolve(req.ID, strings.HasPrefix(strings.TrimSpace(ans), "y"))
		d := <-decisionCh
		fmt.Printf("decision: %v (denied3x=%v)\n", d, g.Denied3x)
	}
}
