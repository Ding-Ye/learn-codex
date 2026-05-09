package s09

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"` // string|number|null
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is the JSON-RPC error envelope.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("rpc %d: %s", e.Code, e.Message) }

// Conn is a duplex JSON-RPC over a delimited transport.
//
// MCP wire format on stdio is "newline-delimited JSON" — one JSON object per
// line (LF-separated). This is the simplest possible framing.
type Conn struct {
	in  io.WriteCloser
	out *bufio.Reader

	mu       sync.Mutex
	pending  map[uint64]chan *Response
	nextID   atomic.Uint64
	notifyCh chan *Request
}

// NewConn constructs a Conn from a stdin (write to peer) and stdout (read
// from peer) pair. Spawn a goroutine via Run() to drive the read loop.
func NewConn(stdin io.WriteCloser, stdout io.Reader) *Conn {
	return &Conn{
		in:       stdin,
		out:      bufio.NewReaderSize(stdout, 1<<20),
		pending:  map[uint64]chan *Response{},
		notifyCh: make(chan *Request, 16),
	}
}

// Run drains stdout. Returns when the underlying reader hits EOF or errors.
func (c *Conn) Run() error {
	for {
		line, err := c.out.ReadBytes('\n')
		if len(line) > 0 {
			var probe struct {
				ID     any             `json:"id"`
				Method string          `json:"method"`
				Result json.RawMessage `json:"result"`
				Error  *RPCError       `json:"error"`
			}
			if jerr := json.Unmarshal(line, &probe); jerr != nil {
				continue // skip non-json garbage
			}
			if probe.Method != "" && probe.ID == nil {
				// notification from peer (e.g. server-initiated)
				var req Request
				_ = json.Unmarshal(line, &req)
				select {
				case c.notifyCh <- &req:
				default:
				}
				continue
			}
			// response to one of our requests
			id, _ := probe.ID.(float64) // JSON numbers come back as float64
			c.mu.Lock()
			ch, ok := c.pending[uint64(id)]
			delete(c.pending, uint64(id))
			c.mu.Unlock()
			if ok {
				resp := Response{ID: probe.ID, Result: probe.Result, Error: probe.Error, JSONRPC: "2.0"}
				ch <- &resp
			}
		}
		if err != nil {
			return err
		}
	}
}

// Notifications returns a channel of server-initiated requests.
// (MCP servers can push e.g. progress updates this way.)
func (c *Conn) Notifications() <-chan *Request { return c.notifyCh }

// Call sends a request and blocks until the response arrives.
func (c *Conn) Call(method string, params any) (*Response, error) {
	id := c.nextID.Add(1)
	var pb json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		pb = b
	}
	req := Request{JSONRPC: "2.0", ID: id, Method: method, Params: pb}
	line, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	ch := make(chan *Response, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	if _, err := c.in.Write(append(line, '\n')); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	resp := <-ch
	if resp.Error != nil {
		return resp, resp.Error
	}
	return resp, nil
}

// Notify sends a one-way request (no id, no response expected).
func (c *Conn) Notify(method string, params any) error {
	var pb json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		pb = b
	}
	req := Request{JSONRPC: "2.0", Method: method, Params: pb}
	line, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = c.in.Write(append(line, '\n'))
	return err
}

// Close shuts down the connection.
func (c *Conn) Close() error { return c.in.Close() }
