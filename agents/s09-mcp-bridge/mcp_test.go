package s09

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakePipe wires two in-process pipes so the client and a fake server can
// exchange newline-delimited JSON without any OS process or socket.
//
// Returns:
//   - clientStdin  : client → server
//   - clientStdout : server → client
//   - serverHandler launches a goroutine reading clientStdin's far end and
//                   writing replies into clientStdout's far end
type fakeServer struct {
	tools          []ToolDef
	callImpl       func(name string, args json.RawMessage) ToolsCallResult
	requireInitAck bool
	initAcked      sync.Mutex
	initAckChan    chan struct{}
}

func runFake(t *testing.T, srv *fakeServer) (in io.WriteCloser, out io.Reader) {
	t.Helper()
	clientToServer := make(chan []byte, 16)
	serverToClient := make(chan []byte, 16)

	cs := &chanWriter{ch: clientToServer}
	cr := &chanReader{ch: serverToClient}
	srv.initAckChan = make(chan struct{}, 1)
	srv.initAcked.Lock()

	go func() {
		defer close(serverToClient)
		for line := range clientToServer {
			var req struct {
				ID     any             `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(line, &req); err != nil {
				continue
			}
			switch req.Method {
			case "initialize":
				resp := Response{JSONRPC: "2.0", ID: req.ID, Result: mustMarshal(t, InitializeResult{
					ProtocolVersion: "2024-11-05",
					ServerInfo:      ServerInfo{Name: "fake", Version: "1.0"},
				})}
				serverToClient <- mustMarshal(t, resp)
				serverToClient <- []byte{'\n'}
			case "notifications/initialized":
				select {
				case srv.initAckChan <- struct{}{}:
				default:
				}
			case "tools/list":
				resp := Response{JSONRPC: "2.0", ID: req.ID, Result: mustMarshal(t, ToolsListResult{Tools: srv.tools})}
				serverToClient <- mustMarshal(t, resp)
				serverToClient <- []byte{'\n'}
			case "tools/call":
				var p ToolsCallParams
				_ = json.Unmarshal(req.Params, &p)
				result := srv.callImpl(p.Name, p.Arguments)
				resp := Response{JSONRPC: "2.0", ID: req.ID, Result: mustMarshal(t, result)}
				serverToClient <- mustMarshal(t, resp)
				serverToClient <- []byte{'\n'}
			default:
				resp := Response{JSONRPC: "2.0", ID: req.ID, Error: &RPCError{Code: -32601, Message: "method not found"}}
				serverToClient <- mustMarshal(t, resp)
				serverToClient <- []byte{'\n'}
			}
		}
	}()

	// Combine the small payload chunks into a single line stream the client expects.
	go func() {
		var buf []byte
		for chunk := range serverToClient {
			buf = append(buf, chunk...)
			if i := lastNewline(buf); i >= 0 {
				cr.feed(buf[:i+1])
				buf = buf[i+1:]
			}
		}
		cr.close()
	}()

	return cs, bufio.NewReader(cr)
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// chanWriter / chanReader implement io.WriteCloser / io.Reader over channels.

type chanWriter struct {
	ch     chan []byte
	mu     sync.Mutex
	closed bool
}

func (c *chanWriter) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, io.ErrClosedPipe
	}
	cp := append([]byte(nil), p...)
	c.ch <- cp
	return len(p), nil
}

func (c *chanWriter) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.ch)
	}
	return nil
}

type chanReader struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	closed bool
	ch     chan []byte
}

func (c *chanReader) feed(p []byte) {
	c.mu.Lock()
	if c.cond == nil {
		c.cond = sync.NewCond(&c.mu)
	}
	c.buf = append(c.buf, p...)
	c.cond.Broadcast()
	c.mu.Unlock()
}

func (c *chanReader) close() {
	c.mu.Lock()
	if c.cond == nil {
		c.cond = sync.NewCond(&c.mu)
	}
	c.closed = true
	c.cond.Broadcast()
	c.mu.Unlock()
}

func (c *chanReader) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cond == nil {
		c.cond = sync.NewCond(&c.mu)
	}
	for len(c.buf) == 0 && !c.closed {
		c.cond.Wait()
	}
	if len(c.buf) == 0 && c.closed {
		return 0, io.EOF
	}
	n := copy(p, c.buf)
	c.buf = c.buf[n:]
	return n, nil
}

func lastNewline(buf []byte) int {
	for i := len(buf) - 1; i >= 0; i-- {
		if buf[i] == '\n' {
			return i
		}
	}
	return -1
}

// ---------------- the actual tests ----------------

func TestJSONRPCRequestResponse(t *testing.T) {
	srv := &fakeServer{
		tools: []ToolDef{{Name: "echo", Description: "", InputSchema: json.RawMessage(`{}`)}},
		callImpl: func(name string, args json.RawMessage) ToolsCallResult {
			return ToolsCallResult{Content: []ToolContent{{Type: "text", Text: "hi"}}}
		},
	}
	in, out := runFake(t, srv)
	conn := NewConn(in, out)
	go conn.Run()
	defer conn.Close()

	resp, err := conn.Call("tools/list", map[string]any{})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if resp.Error != nil {
		t.Errorf("error: %v", resp.Error)
	}
	if !strings.Contains(string(resp.Result), `"echo"`) {
		t.Errorf("missing echo in result: %q", string(resp.Result))
	}
}

func TestMCPInitializeAndListTools(t *testing.T) {
	srv := &fakeServer{
		tools: []ToolDef{
			{Name: "shell", Description: "run a shell command", InputSchema: json.RawMessage(`{}`)},
			{Name: "fs.read", Description: "read a file", InputSchema: json.RawMessage(`{}`)},
			{Name: "fs.write", Description: "write a file", InputSchema: json.RawMessage(`{}`)},
		},
		callImpl: func(string, json.RawMessage) ToolsCallResult { return ToolsCallResult{} },
	}
	in, out := runFake(t, srv)
	conn := NewConn(in, out)
	go conn.Run()
	defer conn.Close()
	client := NewClient(conn)

	res, err := client.Initialize("learn-codex", "0.1")
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if res.ServerInfo.Name != "fake" {
		t.Errorf("server name = %q want fake", res.ServerInfo.Name)
	}

	// Confirm initialized notification was sent.
	select {
	case <-srv.initAckChan:
	case <-time.After(time.Second):
		t.Errorf("server didn't see initialized notification")
	}

	tools, err := client.ListTools()
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(tools) != 3 {
		t.Errorf("tools=%d want 3", len(tools))
	}
}

func TestMCPCallToolEcho(t *testing.T) {
	srv := &fakeServer{
		tools: []ToolDef{{Name: "echo", InputSchema: json.RawMessage(`{}`)}},
		callImpl: func(name string, args json.RawMessage) ToolsCallResult {
			return ToolsCallResult{Content: []ToolContent{{Type: "text", Text: "args=" + string(args)}}}
		},
	}
	in, out := runFake(t, srv)
	conn := NewConn(in, out)
	go conn.Run()
	defer conn.Close()
	client := NewClient(conn)
	if _, err := client.Initialize("learn-codex", "0.1"); err != nil {
		t.Fatalf("init: %v", err)
	}
	res, err := client.CallTool("echo", json.RawMessage(`{"hello":"world"}`))
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if len(res.Content) != 1 || !strings.Contains(res.Content[0].Text, `"hello":"world"`) {
		t.Errorf("unexpected result: %+v", res)
	}
}
