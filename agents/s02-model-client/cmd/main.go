package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	s02 "github.com/Ding-Ye/learn-codex/agents/s02-model-client"
)

// REPL that calls a real OpenAI Chat Completions endpoint.
//
//   OPENAI_API_KEY=sk-...    go run ./cmd
//   OPENAI_BASE_URL=...      go run ./cmd  # e.g. local proxy
//   OPENAI_MODEL=gpt-4o-mini go run ./cmd
//
// Tool calls are surfaced as EvToolCallRequested events but NOT executed —
// that's s03's job.
func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("set OPENAI_API_KEY to talk to a real model")
		os.Exit(2)
	}
	model := envOr("OPENAI_MODEL", "gpt-4o-mini")
	base := envOr("OPENAI_BASE_URL", "https://api.openai.com/v1")

	provider := s02.WithEndpoint(base, apiKey)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := s02.New(ctx, provider, model, "You are a helpful coding assistant.")
	go printer(c.Events())

	fmt.Printf("learn-codex s02 model-client (model=%s). '/quit' to exit.\n", model)
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		switch line {
		case "":
		case "/quit":
			c.Submit(s02.OpShutdown{})
			<-ctx.Done()
			return
		default:
			c.Submit(s02.OpUserInput{Text: line})
		}
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func printer(events <-chan s02.EventMsg) {
	for ev := range events {
		switch e := ev.(type) {
		case s02.EvTurnStarted:
			fmt.Printf("\n  [turn %s started]\n", e.TurnID)
		case s02.EvAgentMessage:
			fmt.Print(e.Text) // streaming chunks; no newline
		case s02.EvToolCallRequested:
			fmt.Printf("\n  [tool call: %s]\n", e.Call.String())
		case s02.EvTurnComplete:
			fmt.Printf("\n  [turn %s done]\n", e.TurnID)
		case s02.EvError:
			fmt.Printf("\n  ERROR: %s\n", e.Message)
		}
	}
}
