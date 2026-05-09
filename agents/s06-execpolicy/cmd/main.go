package main

import (
	"fmt"
	"os"

	s06 "github.com/Ding-Ye/learn-codex/agents/s06-execpolicy"
)

// usage: classify <rules-file> -- <cmd...>
//
//	classify testdata/default.rules -- git status
//	classify testdata/default.rules -- rm -rf /tmp
func main() {
	if len(os.Args) < 4 || os.Args[2] != "--" {
		fmt.Fprintln(os.Stderr, "usage: classify <rules-file> -- <cmd...>")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	policy, err := s06.Parse(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	d, rule := policy.Classify(os.Args[3:])
	if rule != nil {
		fmt.Printf("decision=%s reason=%q rule=%q\n", d, rule.Reason, rule.Tokens)
	} else {
		fmt.Printf("decision=%s (default)\n", d)
	}
}
