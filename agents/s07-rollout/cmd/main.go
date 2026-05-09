package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	s07 "github.com/Ding-Ye/learn-codex/agents/s07-rollout"
)

func main() {
	mode := flag.String("mode", "record", "record | replay")
	path := flag.String("path", "/tmp/learn-codex-session.jsonl", "rollout file path")
	flag.Parse()

	switch *mode {
	case "record":
		_ = os.MkdirAll(filepath.Dir(*path), 0o755)
		r, err := s07.New(*path, s07.SessionMeta{ID: fmt.Sprintf("sess-%d", time.Now().Unix()), Cwd: "/tmp", Model: "demo"})
		if err != nil {
			panic(err)
		}
		_ = r.Record(s07.KindUserInput, s07.UserInputItem{Text: "hello"})
		_ = r.Record(s07.KindAssistantText, s07.AssistantTextItem{Text: "Hi! Recorded."})
		_ = r.Close()
		fmt.Println("recorded →", *path)
	case "replay":
		meta, items, err := s07.Replay(*path)
		if err != nil {
			panic(err)
		}
		fmt.Printf("session: %+v\n", meta)
		for _, it := range items {
			fmt.Printf("  %s (%dms): %s\n", it.Kind, it.TS, string(it.Payload))
		}
	}
}
