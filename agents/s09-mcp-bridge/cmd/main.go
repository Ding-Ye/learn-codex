package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"

	s09 "github.com/Ding-Ye/learn-codex/agents/s09-mcp-bridge"
)

// usage:
//   mcp-bridge -- npx -y @modelcontextprotocol/server-everything
//
// Spawns the MCP server, performs initialize, lists tools, and prints them.
func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: mcp-bridge <server-cmd...>")
		os.Exit(2)
	}
	cmd := exec.Command(args[0], args[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		panic(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		panic(err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		panic(err)
	}

	conn := s09.NewConn(stdin, stdout)
	go func() { _ = conn.Run() }()

	client := s09.NewClient(conn)
	res, err := client.Initialize("learn-codex", "0.1")
	if err != nil {
		fmt.Fprintln(os.Stderr, "init:", err)
		_ = stdin.Close()
		_ = cmd.Wait()
		os.Exit(1)
	}
	fmt.Printf("server: %s %s\n", res.ServerInfo.Name, res.ServerInfo.Version)

	tools, err := client.ListTools()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tools/list:", err)
	}
	for _, t := range tools {
		fmt.Printf("  - %s: %s\n", t.Name, t.Description)
	}

	_ = stdin.Close()
	_, _ = io.Copy(io.Discard, stdout)
	_ = cmd.Wait()
}
