package s01

import (
	"context"
	"fmt"
	"sync/atomic"
)

// Codex is one conversation. It owns:
//   - an unbounded submission queue (buffered chan)
//   - an event channel the frontend ranges over
//   - a goroutine that drains submissions and emits events
//
// In s02 the goroutine will call a real LLM Provider. In s01 it just echoes.
//
// This is the smallest thing that deserves the name. Upstream Codex layers a
// rollout recorder, a guardian session, an MCP manager, etc. — every one of
// those gets attached *to this same loop* in later sessions.
type Codex struct {
	subs   chan Submission
	events chan EventMsg
	turnID atomic.Uint64
}

// New returns a started Codex. Cancel ctx to stop the loop early; OpShutdown
// is the cooperative way to drain.
func New(ctx context.Context) *Codex {
	c := &Codex{
		subs:   make(chan Submission, 16),
		events: make(chan EventMsg, 16),
	}
	go c.run(ctx)
	return c
}

// Submit enqueues an Op. Returns the assigned submission id.
func (c *Codex) Submit(op Op) string {
	id := fmt.Sprintf("sub-%d", c.turnID.Add(1))
	c.subs <- Submission{ID: id, Op: op}
	return id
}

// Events returns the read-only event stream. The channel closes after
// OpShutdown is processed (and an EvShutdownComplete is emitted).
func (c *Codex) Events() <-chan EventMsg { return c.events }

func (c *Codex) run(ctx context.Context) {
	defer close(c.events)
	for {
		select {
		case <-ctx.Done():
			return
		case sub, ok := <-c.subs:
			if !ok {
				return
			}
			if !c.handle(ctx, sub) {
				// shutdown
				c.events <- EvShutdownComplete{}
				return
			}
		}
	}
}

// handle returns false on shutdown, true otherwise.
func (c *Codex) handle(ctx context.Context, sub Submission) bool {
	switch op := sub.Op.(type) {
	case OpUserInput:
		c.runTurn(ctx, sub.ID, op.Text)
		return true
	case OpInterrupt:
		// In s01 there's nothing in flight to cancel; later sessions check
		// a per-turn context.CancelFunc here.
		return true
	case OpShutdown:
		return false
	default:
		// Unknown ops (e.g. OpExecApproval before s05) become a no-op.
		return true
	}
}

// runTurn is the meat of s01. We emit TurnStarted, "respond" (in s01: echo
// the user's text framed as an assistant message), then TurnComplete.
func (c *Codex) runTurn(ctx context.Context, turnID, text string) {
	c.events <- EvTurnStarted{TurnID: turnID}

	// Cooperative cancellation: between events we honour ctx and any pending
	// OpInterrupt. We poll the queue non-blockingly here.
	if c.peekInterrupt() {
		c.events <- EvTurnComplete{TurnID: turnID, Cancelled: true}
		return
	}

	c.events <- EvAgentMessage{
		TurnID: turnID,
		Text:   "echo: " + text,
	}
	c.events <- EvTurnComplete{TurnID: turnID, Cancelled: false}
}

// peekInterrupt drains any OpInterrupt sitting in the submission queue right
// now. Anything else is put back at the head — well, since chans don't allow
// peek, we do a non-blocking receive and re-enqueue if it wasn't an interrupt.
// Simple, single-consumer, works for s01's single-goroutine semantics.
func (c *Codex) peekInterrupt() bool {
	select {
	case sub := <-c.subs:
		if _, ok := sub.Op.(OpInterrupt); ok {
			return true
		}
		// not an interrupt — put it back
		c.subs <- sub
	default:
	}
	return false
}
