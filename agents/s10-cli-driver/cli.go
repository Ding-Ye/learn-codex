package s10

import (
	"flag"
	"fmt"
	"io"
)

// Subcommand is one top-level `codex <subcommand> ...`.
type Subcommand struct {
	Name        string
	Synopsis    string
	Run         func(args []string, stdin io.Reader, stdout, stderr io.Writer) int
}

// Registry holds the subcommands we can dispatch.
type Registry struct{ Cmds []Subcommand }

func NewRegistry() *Registry { return &Registry{} }

func (r *Registry) Add(c Subcommand) { r.Cmds = append(r.Cmds, c) }

// Dispatch resolves args[0] to a subcommand and runs it.
// args[0] is the subcommand name (NOT the binary path).
//
// Returns the subcommand's exit code, or 2 for usage errors.
func (r *Registry) Dispatch(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		r.printHelp(stderr)
		return 0
	}
	for _, c := range r.Cmds {
		if c.Name == args[0] {
			return c.Run(args[1:], stdin, stdout, stderr)
		}
	}
	fmt.Fprintf(stderr, "unknown subcommand: %q\n", args[0])
	r.printHelp(stderr)
	return 2
}

func (r *Registry) printHelp(w io.Writer) {
	fmt.Fprintln(w, "usage: codex <subcommand> [flags]")
	fmt.Fprintln(w, "subcommands:")
	for _, c := range r.Cmds {
		fmt.Fprintf(w, "  %-12s %s\n", c.Name, c.Synopsis)
	}
}

// SubFlagSet returns a FlagSet pre-configured to write usage to `out`.
func SubFlagSet(name string, out io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(out)
	return fs
}
