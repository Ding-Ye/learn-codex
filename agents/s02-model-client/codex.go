package s02

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
)

// Op / EventMsg are re-declared in this session because each session is a
// self-contained module. The shape matches s01.
type Op interface{ isOp() }
type OpUserInput struct{ Text string }
type OpInterrupt struct{}
type OpShutdown struct{}

func (OpUserInput) isOp() {}
func (OpInterrupt) isOp() {}
func (OpShutdown) isOp()  {}

type EventMsg interface{ isEventMsg() }
type EvTurnStarted struct{ TurnID string }
type EvAgentMessage struct {
	TurnID string
	Text   string
}
type EvToolCallRequested struct {
	TurnID string
	Call   ToolCall
}
type EvTurnComplete struct {
	TurnID    string
	Cancelled bool
}
type EvShutdownComplete struct{}
type EvError struct{ Message string }

func (EvTurnStarted) isEventMsg()       {}
func (EvAgentMessage) isEventMsg()      {}
func (EvToolCallRequested) isEventMsg() {}
func (EvTurnComplete) isEventMsg()      {}
func (EvShutdownComplete) isEventMsg()  {}
func (EvError) isEventMsg()             {}

// Codex is now wired to a Provider. The loop:
//   1. on OpUserInput, build the message list (system + history + user)
//   2. call provider.Stream → forward ProvText as EvAgentMessage,
//      ProvToolCall as EvToolCallRequested
//   3. emit EvTurnComplete
//
// Tool execution itself is the next session (s03). For s02 we just *surface*
// any tool calls so the lesson is "the agent now talks to a real LLM".
type Codex struct {
	provider Provider
	model    string
	system   string

	subs   chan Submission
	events chan EventMsg
	turnID atomic.Uint64

	history []ChatMessage
}

type Submission struct {
	ID string
	Op Op
}

func New(ctx context.Context, p Provider, model, system string) *Codex {
	c := &Codex{
		provider: p,
		model:    model,
		system:   system,
		subs:     make(chan Submission, 16),
		events:   make(chan EventMsg, 16),
	}
	if system != "" {
		c.history = []ChatMessage{{Role: "system", Content: system}}
	}
	go c.run(ctx)
	return c
}

func (c *Codex) Submit(op Op) string {
	id := fmt.Sprintf("sub-%d", c.turnID.Add(1))
	c.subs <- Submission{ID: id, Op: op}
	return id
}

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
			switch op := sub.Op.(type) {
			case OpUserInput:
				c.runTurn(ctx, sub.ID, op.Text)
			case OpInterrupt:
				// no-op between turns; s05 wires per-turn ctx
			case OpShutdown:
				c.events <- EvShutdownComplete{}
				return
			}
		}
	}
}

func (c *Codex) runTurn(ctx context.Context, turnID, text string) {
	c.events <- EvTurnStarted{TurnID: turnID}

	c.history = append(c.history, ChatMessage{Role: "user", Content: text})

	out := make(chan ProviderEvent, 16)
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.provider.Stream(ctx, ProviderRequest{
			Model:    c.model,
			Messages: c.history,
		}, out)
		close(out)
	}()

	var assistantText strings.Builder
	var calls []ToolCall

	for ev := range out {
		switch e := ev.(type) {
		case ProvText:
			assistantText.WriteString(e.Delta)
			c.events <- EvAgentMessage{TurnID: turnID, Text: e.Delta}
		case ProvToolCall:
			calls = append(calls, e.Call)
			c.events <- EvToolCallRequested{TurnID: turnID, Call: e.Call}
		case ProvError:
			c.events <- EvError{Message: e.Err.Error()}
		case ProvDone:
			// finish reason logged in EvTurnComplete via the err channel
		}
	}
	if err := <-errCh; err != nil {
		c.events <- EvError{Message: err.Error()}
	}

	// Append the assistant turn to history (with any tool calls).
	asMsg := ChatMessage{Role: "assistant", Content: assistantText.String()}
	if len(calls) > 0 {
		asMsg.ToolCalls = calls
	}
	c.history = append(c.history, asMsg)

	c.events <- EvTurnComplete{TurnID: turnID, Cancelled: false}
}
