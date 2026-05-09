package s02

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeSSE serves a canned chunked response.
func fakeSSE(chunks []string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			_, _ = w.Write([]byte("data: " + c + "\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
}

func collect(t *testing.T, out <-chan ProviderEvent) []ProviderEvent {
	t.Helper()
	var got []ProviderEvent
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-out:
			if !ok {
				return got
			}
			got = append(got, ev)
		case <-deadline:
			t.Fatalf("timeout; got=%v", got)
		}
	}
}

func TestSSEParsesTextDeltas(t *testing.T) {
	srv := fakeSSE([]string{
		`{"choices":[{"delta":{"content":"Hello"}}]}`,
		`{"choices":[{"delta":{"content":", world"}}]}`,
		`{"choices":[{"delta":{"content":""},"finish_reason":"stop"}]}`,
	})
	defer srv.Close()

	p := WithEndpoint(srv.URL, "")
	out := make(chan ProviderEvent, 8)
	done := make(chan error, 1)
	go func() { done <- p.Stream(context.Background(), ProviderRequest{Model: "gpt-test"}, out); close(out) }()

	got := collect(t, out)
	if err := <-done; err != nil {
		t.Fatalf("Stream err: %v", err)
	}

	var sb strings.Builder
	var sawDone bool
	for _, ev := range got {
		switch e := ev.(type) {
		case ProvText:
			sb.WriteString(e.Delta)
		case ProvDone:
			sawDone = true
			if e.FinishReason != "stop" {
				t.Errorf("FinishReason=%q want stop", e.FinishReason)
			}
		}
	}
	if sb.String() != "Hello, world" {
		t.Errorf("text=%q want Hello, world", sb.String())
	}
	if !sawDone {
		t.Errorf("no ProvDone event")
	}
}

func TestSSEParsesToolCall(t *testing.T) {
	srv := fakeSSE([]string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"shell","arguments":"{\"cmd\":\"go"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":" version\"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	})
	defer srv.Close()

	p := WithEndpoint(srv.URL, "")
	out := make(chan ProviderEvent, 8)
	done := make(chan error, 1)
	go func() { done <- p.Stream(context.Background(), ProviderRequest{Model: "gpt-test"}, out); close(out) }()

	got := collect(t, out)
	if err := <-done; err != nil {
		t.Fatalf("Stream err: %v", err)
	}

	var calls []ToolCall
	for _, ev := range got {
		if tc, ok := ev.(ProvToolCall); ok {
			calls = append(calls, tc.Call)
		}
	}
	if len(calls) != 1 {
		t.Fatalf("want 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "shell" {
		t.Errorf("name=%q want shell", calls[0].Function.Name)
	}

	var args struct{ Cmd string }
	if err := json.Unmarshal(calls[0].Function.Args, &args); err != nil {
		t.Fatalf("args parse: %v (raw=%q)", err, string(calls[0].Function.Args))
	}
	if args.Cmd != "go version" {
		t.Errorf("cmd=%q want %q", args.Cmd, "go version")
	}
}

func TestStreamServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	p := WithEndpoint(srv.URL, "")
	out := make(chan ProviderEvent, 1)
	defer close(out)

	if err := p.Stream(context.Background(), ProviderRequest{}, out); err == nil {
		t.Fatalf("want error on 429, got nil")
	}
}
