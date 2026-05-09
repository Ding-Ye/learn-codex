package s07

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestRecordAndReplayRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "session.jsonl")

	r, err := New(p, SessionMeta{ID: "sess-1", Model: "gpt-test", Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := r.Record(KindUserInput, UserInputItem{Text: "hi"}); err != nil {
		t.Fatalf("record user: %v", err)
	}
	if err := r.Record(KindAssistantText, AssistantTextItem{Text: "hello there"}); err != nil {
		t.Fatalf("record asst: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	meta, items, err := Replay(p)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if meta == nil || meta.ID != "sess-1" {
		t.Errorf("meta=%+v want ID=sess-1", meta)
	}
	if len(items) != 2 {
		t.Fatalf("items=%d want 2", len(items))
	}
	if items[0].Kind != KindUserInput || items[1].Kind != KindAssistantText {
		t.Errorf("kinds = %s, %s; want user_input, assistant_text", items[0].Kind, items[1].Kind)
	}

	msgs := Reconstruct(items)
	if len(msgs) != 2 || msgs[0].Role != "user" || msgs[1].Role != "assistant" {
		t.Errorf("reconstructed=%+v", msgs)
	}
}

func TestResumePreservesPriorMeta(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")

	r, _ := New(p, SessionMeta{ID: "first"})
	_ = r.Record(KindUserInput, UserInputItem{Text: "a"})
	_ = r.Close()

	// Open again with different meta; the new meta line should NOT be added.
	r2, _ := New(p, SessionMeta{ID: "second"})
	_ = r2.Record(KindUserInput, UserInputItem{Text: "b"})
	_ = r2.Close()

	meta, items, _ := Replay(p)
	if meta == nil || meta.ID != "first" {
		t.Errorf("meta=%+v want ID=first (kept from first session)", meta)
	}
	if len(items) != 2 {
		t.Errorf("items=%d want 2", len(items))
	}
}

func TestConcurrentRecord(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")
	r, _ := New(p, SessionMeta{ID: "x"})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(j int) {
			defer wg.Done()
			_ = r.Record(KindUserInput, UserInputItem{Text: "u"})
		}(i)
	}
	wg.Wait()
	_ = r.Close()

	_, items, _ := Replay(p)
	if len(items) != 50 {
		t.Errorf("items=%d want 50 (no concurrent corruption)", len(items))
	}
}

func TestTruncatedLineSkipped(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")
	r, _ := New(p, SessionMeta{ID: "x"})
	_ = r.Record(KindUserInput, UserInputItem{Text: "complete"})
	_ = r.Close()

	// Append a deliberately broken line.
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.Write([]byte(`{"kind":"user_input","ts":0,"payl`)) // no newline, no closing
	_ = f.Close()

	_, items, err := Replay(p)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	// We expect just the one good item; the broken trailing chunk is skipped.
	if len(items) != 1 {
		t.Errorf("items=%d want 1 (truncation tolerated)", len(items))
	}
}
