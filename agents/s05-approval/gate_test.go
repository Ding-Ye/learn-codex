package s05

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPolicyNeverAllowsAll(t *testing.T) {
	g := NewGate(ApprovalNever)
	d := g.Check(context.Background(), Request{ID: "1", Command: []string{"rm", "-rf", "/"}}, func(Request) {
		t.Fatalf("emit should not be called under ApprovalNever")
	})
	if d != DecisionAllow {
		t.Errorf("decision=%v want Allow", d)
	}
}

func TestPolicyOnRequestEmitsAndBlocksUntilResolve(t *testing.T) {
	g := NewGate(ApprovalOnRequest)
	emitted := make(chan Request, 1)
	emit := func(r Request) { emitted <- r }

	go func() {
		r := <-emitted
		if r.ID != "tc-1" {
			t.Errorf("ID=%q want tc-1", r.ID)
		}
		_ = g.Resolve(r.ID, true)
	}()

	d := g.Check(context.Background(), Request{ID: "tc-1", Command: []string{"ls"}}, emit)
	if d != DecisionAllow {
		t.Errorf("decision=%v want Allow after Resolve(true)", d)
	}
}

func TestUnlessTrustedSkipsAllowlist(t *testing.T) {
	g := NewGate(ApprovalUnlessTrusted)
	g.Trusted = func(cmd []string) (bool, string) {
		if len(cmd) > 0 && cmd[0] == "ls" {
			return true, "always-safe"
		}
		return false, "unknown"
	}

	called := false
	emit := func(Request) { called = true }
	d := g.Check(context.Background(), Request{ID: "1", Command: []string{"ls", "/tmp"}}, emit)
	if d != DecisionAllow {
		t.Errorf("ls not allowed by trusted func: %v", d)
	}
	if called {
		t.Errorf("emit should not be called for trusted command")
	}

	// Untrusted command must emit + block (we resolve quickly).
	emitted := make(chan Request, 1)
	emit = func(r Request) { emitted <- r }
	go func() { r := <-emitted; _ = g.Resolve(r.ID, false) }()
	d = g.Check(context.Background(), Request{ID: "2", Command: []string{"rm"}}, emit)
	if d != DecisionDeny {
		t.Errorf("decision=%v want Deny after Resolve(false)", d)
	}
}

func TestThreeConsecutiveDenialsTriggers3x(t *testing.T) {
	g := NewGate(ApprovalOnRequest)
	emit := func(Request) {} // ignore — we'll resolve directly

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		id := []string{"a", "b", "c"}[i]
		wg.Add(1)
		go func(rid string) {
			defer wg.Done()
			_ = g.Check(context.Background(), Request{ID: rid, Command: []string{"x"}}, emit)
		}(id)
		// Give Check time to register the pending entry.
		time.Sleep(20 * time.Millisecond)
		_ = g.Resolve(id, false)
	}
	wg.Wait()

	if !g.Denied3x {
		t.Errorf("Denied3x not triggered after 3 consecutive denies")
	}
}

func TestCtxCancelDeniesGracefully(t *testing.T) {
	g := NewGate(ApprovalOnRequest)
	ctx, cancel := context.WithCancel(context.Background())
	emit := func(Request) {}

	done := make(chan Decision, 1)
	go func() { done <- g.Check(ctx, Request{ID: "1", Command: []string{"x"}}, emit) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case d := <-done:
		if d != DecisionDeny {
			t.Errorf("on ctx cancel decision=%v want Deny", d)
		}
	case <-time.After(time.Second):
		t.Fatalf("Check did not return after ctx cancel")
	}
}
