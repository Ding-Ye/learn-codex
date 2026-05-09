package s02

import (
	"context"
	"testing"
	"time"
)

func TestEchoProviderEmitsScript(t *testing.T) {
	p := EchoText("Hello, world")
	out := make(chan ProviderEvent, 4)
	done := make(chan error, 1)
	go func() {
		done <- p.Stream(context.Background(), ProviderRequest{Model: "echo"}, out)
		close(out)
	}()
	var sb string
	var sawDone bool
	for ev := range out {
		switch e := ev.(type) {
		case ProvText:
			sb += e.Delta
		case ProvDone:
			sawDone = true
		}
	}
	if err := <-done; err != nil {
		t.Fatalf("stream: %v", err)
	}
	if sb != "Hello, world" {
		t.Errorf("text=%q want Hello, world", sb)
	}
	if !sawDone {
		t.Errorf("no ProvDone")
	}
}

func TestEchoProviderRespectsContext(t *testing.T) {
	p := &EchoProvider{Script: []ProviderEvent{ProvText{Delta: "a"}, ProvText{Delta: "b"}}}
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan ProviderEvent) // unbuffered → writer blocks
	done := make(chan error, 1)
	go func() {
		done <- p.Stream(ctx, ProviderRequest{}, out)
		close(out)
	}()
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("err=%v want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("Stream didn't honour ctx cancel")
	}
}
