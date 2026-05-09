package s01

import (
	"context"
	"testing"
	"time"
)

// drain pulls events off the channel until either the predicate matches or
// the deadline expires, returning everything seen.
func drain(t *testing.T, c *Codex, until func(EventMsg) bool) []EventMsg {
	t.Helper()
	var got []EventMsg
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-c.Events():
			if !ok {
				return got
			}
			got = append(got, ev)
			if until != nil && until(ev) {
				return got
			}
		case <-deadline:
			t.Fatalf("timed out; got=%v", got)
			return got
		}
	}
}

func TestSubmitProducesTurnStartedAndComplete(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := New(ctx)

	c.Submit(OpUserInput{Text: "hello"})

	events := drain(t, c, func(ev EventMsg) bool {
		_, done := ev.(EvTurnComplete)
		return done
	})

	if len(events) < 3 {
		t.Fatalf("want at least 3 events (Started, Message, Complete); got %d: %v", len(events), events)
	}
	if _, ok := events[0].(EvTurnStarted); !ok {
		t.Errorf("first event = %T, want EvTurnStarted", events[0])
	}
	msg, ok := events[1].(EvAgentMessage)
	if !ok {
		t.Errorf("second event = %T, want EvAgentMessage", events[1])
	} else if msg.Text != "echo: hello" {
		t.Errorf("text = %q, want %q", msg.Text, "echo: hello")
	}
	last := events[len(events)-1]
	tc, ok := last.(EvTurnComplete)
	if !ok {
		t.Errorf("last event = %T, want EvTurnComplete", last)
	} else if tc.Cancelled {
		t.Errorf("Cancelled = true, want false")
	}
}

func TestInterruptStopsTurnEarly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := New(ctx)

	// Queue both ops before run() pulls; the interrupt should pre-empt the
	// AgentMessage emission.
	c.Submit(OpUserInput{Text: "hello"})
	c.Submit(OpInterrupt{})

	events := drain(t, c, func(ev EventMsg) bool {
		_, done := ev.(EvTurnComplete)
		return done
	})

	for _, ev := range events {
		if _, isMsg := ev.(EvAgentMessage); isMsg {
			t.Errorf("got AgentMessage despite interrupt: %v", events)
		}
	}
	last := events[len(events)-1].(EvTurnComplete)
	if !last.Cancelled {
		t.Errorf("Cancelled = false, want true")
	}
}

func TestShutdownClosesEventChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := New(ctx)

	c.Submit(OpShutdown{})

	events := drain(t, c, nil) // drain until close
	if len(events) == 0 {
		t.Fatalf("want at least EvShutdownComplete, got nothing")
	}
	if _, ok := events[len(events)-1].(EvShutdownComplete); !ok {
		t.Errorf("last event = %T, want EvShutdownComplete", events[len(events)-1])
	}
}

func TestSubmitReturnsUniqueIDs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := New(ctx)

	a := c.Submit(OpUserInput{Text: "a"})
	b := c.Submit(OpUserInput{Text: "b"})
	if a == b {
		t.Errorf("ids collided: %s == %s", a, b)
	}
}
