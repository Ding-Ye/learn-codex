package main

import (
	"fmt"
	"io"
	"os"

	s04 "github.com/Ding-Ye/learn-codex/agents/s04-apply-patch"
)

// `apply-patch < patch.txt` reads a V4A patch on stdin and applies it
// relative to the current directory.
func main() {
	patchText, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read stdin:", err)
		os.Exit(2)
	}
	p, err := s04.Parse(string(patchText))
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		os.Exit(2)
	}
	cwd, _ := os.Getwd()
	changed, err := s04.Apply(cwd, p)
	if err != nil {
		fmt.Fprintln(os.Stderr, "apply:", err)
		os.Exit(1)
	}
	for _, f := range changed {
		fmt.Println(f)
	}
}
