package s02

import (
	"context"
	"strings"
	"testing"
	"time"
)

func drainCodex(t *testing.T, c *Codex, until func(EventMsg) bool) []EventMsg {
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
			t.Fatalf("timeout; got=%v", got)
		}
	}
}

func TestProviderForwardsAsAgentMessage(t *testing.T) {
	srv := fakeSSE([]string{
		`{"choices":[{"delta":{"content":"Hello"}}]}`,
		`{"choices":[{"delta":{"content":" there"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := New(ctx, WithEndpoint(srv.URL, ""), "gpt-test", "")
	c.Submit(OpUserInput{Text: "hi"})

	events := drainCodex(t, c, func(ev EventMsg) bool {
		_, done := ev.(EvTurnComplete)
		return done
	})

	var sb strings.Builder
	for _, ev := range events {
		if msg, ok := ev.(EvAgentMessage); ok {
			sb.WriteString(msg.Text)
		}
	}
	if sb.String() != "Hello there" {
		t.Errorf("text=%q want Hello there", sb.String())
	}
}

func TestProviderToolCallSurfacedAsEvent(t *testing.T) {
	srv := fakeSSE([]string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"shell","arguments":"{\"cmd\":\"ls\"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := New(ctx, WithEndpoint(srv.URL, ""), "gpt-test", "")
	c.Submit(OpUserInput{Text: "list"})

	events := drainCodex(t, c, func(ev EventMsg) bool {
		_, done := ev.(EvTurnComplete)
		return done
	})

	var foundCall bool
	for _, ev := range events {
		if call, ok := ev.(EvToolCallRequested); ok {
			foundCall = true
			if call.Call.Function.Name != "shell" {
				t.Errorf("name=%q want shell", call.Call.Function.Name)
			}
		}
	}
	if !foundCall {
		t.Errorf("no EvToolCallRequested in events")
	}
}
