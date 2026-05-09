package s10

import (
	"fmt"
	"io"
	"strings"
)

// Slash is one in-REPL slash command (`/help`, `/quit`, …).
//
// Returns:
//   - handled: true if the input matched a slash command (caller should NOT
//     forward it to the agent).
//   - shouldQuit: true if the REPL loop should stop.
type Slash struct {
	Name string
	Help string
	Run  func(args []string, out io.Writer) (shouldQuit bool)
}

// SlashSet is the REPL's set of slash commands.
type SlashSet struct {
	Cmds []Slash
}

func NewSlashSet() *SlashSet { return &SlashSet{} }

func (s *SlashSet) Add(cmd Slash) { s.Cmds = append(s.Cmds, cmd) }

// Match parses one input line and dispatches if it begins with `/`.
// Returns (handled, shouldQuit).
func (s *SlashSet) Match(line string, out io.Writer) (handled, shouldQuit bool) {
	if !strings.HasPrefix(line, "/") {
		return false, false
	}
	parts := strings.Fields(line[1:])
	if len(parts) == 0 {
		return true, false
	}
	for _, c := range s.Cmds {
		if c.Name == parts[0] {
			return true, c.Run(parts[1:], out)
		}
	}
	fmt.Fprintf(out, "unknown command /%s; try /help\n", parts[0])
	return true, false
}

// DefaultSet returns /help and /quit pre-installed.
func DefaultSet() *SlashSet {
	s := NewSlashSet()
	s.Add(Slash{
		Name: "help",
		Help: "list slash commands",
		Run: func(_ []string, out io.Writer) bool {
			fmt.Fprintln(out, "slash commands:")
			for _, c := range s.Cmds {
				fmt.Fprintf(out, "  /%-10s %s\n", c.Name, c.Help)
			}
			return false
		},
	})
	s.Add(Slash{
		Name: "quit",
		Help: "exit the REPL",
		Run:  func(_ []string, _ io.Writer) bool { return true },
	})
	return s
}
