package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	s08 "github.com/Ding-Ye/learn-codex/agents/s08-sandbox"
)

func main() {
	readOnly := flag.Bool("ro", false, "read-only sandbox")
	network := flag.Bool("net", false, "allow network")
	root := flag.String("root", "", "writable root (empty=none unless -ro)")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: sandbox -- <cmd...>")
		os.Exit(2)
	}
	args := flag.Args()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	box := s08.New()
	perm := s08.Permissions{ReadOnly: *readOnly, AllowNetwork: *network}
	if *root != "" {
		perm.WritableRoots = []string{*root}
	}
	wrapped, err := box.Wrap(cmd, perm)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wrap:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[sandbox=%s] running: %v\n", box.Name(), args)
	if err := wrapped.Run(); err != nil {
		os.Exit(1)
	}
}
