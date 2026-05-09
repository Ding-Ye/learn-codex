package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	s02 "github.com/Ding-Ye/learn-codex/agents/s02-model-client"
	s07 "github.com/Ding-Ye/learn-codex/agents/s07-rollout"
	s10 "github.com/Ding-Ye/learn-codex/agents/s10-cli-driver"
)

// `codex` is the entry binary stitching s02 (model client) + s07 (rollout)
// together via s10's subcommand + slash registry.
//
// Subcommands:
//   codex                    interactive REPL (default)
//   codex exec "<prompt>"    one-shot, prints the assistant message + exits
//   codex resume <path>      replay an earlier rollout, then continue interactively
//   codex sessions           list rollout files in ~/.codex-learn/sessions
//   codex --help             help
//
// REPL slash commands: /help /quit /policy /sandbox /tools /save
//
// Configuration (env):
//   OPENAI_API_KEY   required
//   OPENAI_MODEL     default gpt-4o-mini
//   OPENAI_BASE_URL  default https://api.openai.com/v1
//   CODEX_HOME       default $HOME/.codex-learn
func main() {
	r := s10.NewRegistry()
	r.Add(s10.Subcommand{Name: "exec", Synopsis: "one-shot prompt", Run: cmdExec})
	r.Add(s10.Subcommand{Name: "resume", Synopsis: "resume a rollout file", Run: cmdResume})
	r.Add(s10.Subcommand{Name: "sessions", Synopsis: "list rollout files", Run: cmdSessions})

	// Default command: interactive
	args := os.Args[1:]
	if len(args) == 0 {
		os.Exit(cmdInteractive(nil, os.Stdin, os.Stdout, os.Stderr))
	}
	switch args[0] {
	case "exec", "resume", "sessions":
		os.Exit(r.Dispatch(args, os.Stdin, os.Stdout, os.Stderr))
	case "--help", "-h", "help":
		os.Exit(r.Dispatch([]string{"help"}, os.Stdin, os.Stdout, os.Stderr))
	default:
		// Treat unknown as a one-shot prompt
		os.Exit(cmdExec(args, os.Stdin, os.Stdout, os.Stderr))
	}
}

// ----- helpers -------------------------------------------------------------

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func newProvider() (*s02.OpenAIChatCompletions, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY not set")
	}
	return s02.WithEndpoint(envOr("OPENAI_BASE_URL", "https://api.openai.com/v1"), key), nil
}

func codexHome() string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex-learn")
}

func newRecorder() (*s07.Recorder, string, error) {
	id := fmt.Sprintf("sess-%d", time.Now().Unix())
	cwd, _ := os.Getwd()
	dir := filepath.Join(codexHome(), "sessions")
	path := filepath.Join(dir, id+".jsonl")
	rec, err := s07.New(path, s07.SessionMeta{
		ID: id, StartedAt: time.Now().UnixMilli(),
		Model: envOr("OPENAI_MODEL", "gpt-4o-mini"), Cwd: cwd,
	})
	return rec, path, err
}

func runOneTurn(ctx context.Context, c *s02.Codex, rec *s07.Recorder, text string, out io.Writer) {
	c.Submit(s02.OpUserInput{Text: text})
	if rec != nil {
		_ = rec.Record(s07.KindUserInput, s07.UserInputItem{Text: text})
	}
	for ev := range c.Events() {
		switch e := ev.(type) {
		case s02.EvAgentMessage:
			fmt.Fprint(out, e.Text)
			if rec != nil {
				_ = rec.Record(s07.KindAssistantText, s07.AssistantTextItem{Text: e.Text})
			}
		case s02.EvToolCallRequested:
			fmt.Fprintf(out, "\n  [tool call requested: %s]\n", e.Call.String())
			if rec != nil {
				_ = rec.Record(s07.KindAssistantToolCall, s07.AssistantToolCallItem{
					ID: e.Call.ID, Name: e.Call.Function.Name, Args: e.Call.Function.Args,
				})
			}
		case s02.EvTurnComplete:
			fmt.Fprintln(out)
			return
		case s02.EvError:
			fmt.Fprintf(out, "\n  ERROR: %s\n", e.Message)
			return
		case s02.EvShutdownComplete:
			return
		}
	}
}

// ----- subcommands ---------------------------------------------------------

func cmdExec(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: codex exec <prompt...>")
		return 2
	}
	prov, err := newProvider()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := s02.New(ctx, prov, envOr("OPENAI_MODEL", "gpt-4o-mini"), "You are a helpful coding assistant. Keep replies concise.")
	rec, path, err := newRecorder()
	if err == nil {
		fmt.Fprintf(stderr, "[rollout] %s\n", path)
	}
	defer func() {
		if rec != nil {
			_ = rec.Close()
		}
	}()
	runOneTurn(ctx, c, rec, strings.Join(args, " "), stdout)
	c.Submit(s02.OpShutdown{})
	for range c.Events() {
	}
	return 0
}

func cmdResume(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: codex resume <path>")
		return 2
	}
	meta, items, err := s07.Replay(args[0])
	if err != nil {
		fmt.Fprintln(stderr, "replay:", err)
		return 1
	}
	if meta != nil {
		fmt.Fprintf(stdout, "session: %s (started %d)\n", meta.ID, meta.StartedAt)
	}
	msgs := s07.Reconstruct(items)
	fmt.Fprintf(stdout, "  %d prior messages restored\n", len(msgs))
	for _, m := range msgs {
		fmt.Fprintf(stdout, "  [%s] %s\n", m.Role, string(m.Item.Payload))
	}
	// In a fully wired Codex we would now create s02.Codex and pre-populate
	// its history with these messages, then start the REPL. The teaching
	// version stops here so the lesson stays focused on the rollout shape.
	fmt.Fprintln(stderr, "(resume continues into interactive — see code comment for the wiring point)")
	return 0
}

func cmdSessions(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	dir := filepath.Join(codexHome(), "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		meta, _, err := s07.Replay(path)
		if err != nil {
			fmt.Fprintf(stdout, "  %s (broken: %v)\n", path, err)
			continue
		}
		if meta == nil {
			fmt.Fprintf(stdout, "  %s (no meta)\n", path)
			continue
		}
		fmt.Fprintf(stdout, "  %s  model=%s cwd=%s\n", meta.ID, meta.Model, meta.Cwd)
	}
	return 0
}

func cmdInteractive(_ []string, stdin io.Reader, stdout, stderr io.Writer) int {
	prov, err := newProvider()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := s02.New(ctx, prov, envOr("OPENAI_MODEL", "gpt-4o-mini"), "You are a helpful coding assistant.")
	rec, path, err := newRecorder()
	if err == nil {
		fmt.Fprintf(stderr, "learn-codex (rollout=%s); type /help for slash commands\n", path)
	}
	defer func() {
		if rec != nil {
			_ = rec.Close()
		}
	}()

	slash := s10.DefaultSet()
	scanner := bufio.NewScanner(stdin)
	for {
		fmt.Fprint(stdout, "> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if handled, quit := slash.Match(line, stdout); handled {
			if quit {
				break
			}
			continue
		}
		runOneTurn(ctx, c, rec, line, stdout)
	}
	c.Submit(s02.OpShutdown{})
	for range c.Events() {
	}
	return 0
}
