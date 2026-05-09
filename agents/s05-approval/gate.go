package s05

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// Decision is the gate's verdict for one tool invocation.
type Decision int

const (
	DecisionAllow Decision = iota
	DecisionDeny
)

// Request is what the gate emits to the frontend when it needs approval.
// In a real Codex, this becomes EventMsg::ExecApprovalRequest. Here it's
// the data shape the Gate.Check() function returns via the emit callback.
type Request struct {
	ID      string
	Command []string // for shell tool
	Path    string   // for apply_patch tool
	Reason  string
}

// Trusted is consulted by Check when policy == ApprovalUnlessTrusted /
// Granular. s06 will replace this with the execpolicy DSL.
type Trusted func(command []string) (allowed bool, reason string)

// Gate is the user-in-the-loop guard for risky tool calls.
//
// Usage in the agent loop:
//
//	emit := func(r Request) { codex.events <- EvExecApprovalRequest{...} }
//	resolve := gate.Check(ctx, "shell", req, emit)
//	... later, when frontend sends Op::ExecApproval, the loop calls
//	    gate.Resolve(req.ID, approved). The original Check() unblocks.
type Gate struct {
	Policy   ApprovalPolicy
	Trusted  Trusted // nil unless ApprovalUnlessTrusted/Granular
	Denied3x bool    // set after 3 consecutive denials in a row

	mu        sync.Mutex
	pending   map[string]chan Decision
	denyRun   atomic.Int64
	denyTotal atomic.Int64

	// MaxConsecutiveDenials triggers Denied3x and an interrupt for the turn.
	MaxConsecutiveDenials int
	// MaxTotalDenials triggers Denied3x for the turn.
	MaxTotalDenials int
}

func NewGate(policy ApprovalPolicy) *Gate {
	return &Gate{
		Policy:                policy,
		pending:               map[string]chan Decision{},
		MaxConsecutiveDenials: 3,
		MaxTotalDenials:       10,
	}
}

// Check returns DecisionAllow / DecisionDeny.
//   - ApprovalNever  → always Allow
//   - ApprovalOnRequest → emit Request, block on Resolve
//   - ApprovalUnlessTrusted/Granular → consult Trusted; if not trusted, emit + block
//
// `ctx` cancellation aborts the wait and returns DecisionDeny.
func (g *Gate) Check(ctx context.Context, req Request, emit func(Request)) Decision {
	switch g.Policy {
	case ApprovalNever:
		return DecisionAllow
	case ApprovalUnlessTrusted, ApprovalGranular:
		if g.Trusted != nil {
			ok, reason := g.Trusted(req.Command)
			if ok {
				return DecisionAllow
			}
			req.Reason = reason
		}
	}

	ch := make(chan Decision, 1)
	g.mu.Lock()
	g.pending[req.ID] = ch
	g.mu.Unlock()

	emit(req)

	select {
	case d := <-ch:
		g.afterDecision(d)
		return d
	case <-ctx.Done():
		g.mu.Lock()
		delete(g.pending, req.ID)
		g.mu.Unlock()
		return DecisionDeny
	}
}

// Resolve is called by the agent loop when the frontend sends an approval Op.
func (g *Gate) Resolve(requestID string, approved bool) error {
	g.mu.Lock()
	ch, ok := g.pending[requestID]
	delete(g.pending, requestID)
	g.mu.Unlock()
	if !ok {
		return fmt.Errorf("no pending approval for id=%s", requestID)
	}
	if approved {
		ch <- DecisionAllow
	} else {
		ch <- DecisionDeny
	}
	return nil
}

func (g *Gate) afterDecision(d Decision) {
	if d == DecisionDeny {
		consec := g.denyRun.Add(1)
		total := g.denyTotal.Add(1)
		if consec >= int64(g.MaxConsecutiveDenials) || total >= int64(g.MaxTotalDenials) {
			g.Denied3x = true
		}
	} else {
		g.denyRun.Store(0)
	}
}

// ResetCounters is called at the start of each turn.
func (g *Gate) ResetCounters() {
	g.denyRun.Store(0)
	g.denyTotal.Store(0)
	g.Denied3x = false
}
