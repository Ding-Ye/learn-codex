package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	s01 "github.com/Ding-Ye/learn-codex/agents/s01-minimum-loop"
)

// A 50-line REPL that exercises s01's loop. No LLM call. The whole point of
// s01 is to feel the protocol shape: type a line → see TurnStarted →
// AgentMessage → TurnComplete events, then loop.
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	c := s01.New(ctx)

	go printer(c.Events())

	fmt.Println("learn-codex s01 minimum loop. Type a line; '/quit' to exit.")
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		switch line {
		case "":
			continue
		case "/quit":
			c.Submit(s01.OpShutdown{})
			<-ctx.Done()
			return
		case "/interrupt":
			c.Submit(s01.OpInterrupt{})
		default:
			c.Submit(s01.OpUserInput{Text: line})
		}
	}
}

func printer(events <-chan s01.EventMsg) {
	for ev := range events {
		switch e := ev.(type) {
		case s01.EvTurnStarted:
			fmt.Printf("  [turn %s started]\n", e.TurnID)
		case s01.EvAgentMessage:
			fmt.Printf("  agent: %s\n", e.Text)
		case s01.EvTurnComplete:
			if e.Cancelled {
				fmt.Printf("  [turn %s cancelled]\n", e.TurnID)
			} else {
				fmt.Printf("  [turn %s done]\n", e.TurnID)
			}
		case s01.EvShutdownComplete:
			fmt.Println("  [shutdown]")
		case s01.EvError:
			fmt.Printf("  ERROR: %s\n", e.Message)
		}
	}
}
