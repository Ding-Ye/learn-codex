package s09

import (
	"encoding/json"
	"fmt"
)

// MCP wire shapes — the subset we need to bridge tools.

// InitializeParams is sent by the client right after connecting.
type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	ClientInfo      ClientInfo     `json:"clientInfo"`
	Capabilities    map[string]any `json:"capabilities,omitempty"`
}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is the server's reply.
type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	ServerInfo      ServerInfo     `json:"serverInfo"`
	Capabilities    map[string]any `json:"capabilities,omitempty"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ToolDef as returned by the server's tools/list.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type ToolsListResult struct {
	Tools []ToolDef `json:"tools"`
}

// ToolsCallParams is the body for tools/call.
type ToolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ToolsCallResult is the body returned by tools/call.
//
// MCP uses a `content` array with typed blocks; we reduce to text-only here.
type ToolsCallResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type ToolContent struct {
	Type string `json:"type"` // "text" | "image" | …
	Text string `json:"text,omitempty"`
}

// Client is a thin wrapper around Conn that speaks MCP.
type Client struct {
	conn   *Conn
	server ServerInfo
}

// Initialize performs the MCP handshake.
func (c *Client) Initialize(name, version string) (*InitializeResult, error) {
	resp, err := c.conn.Call("initialize", InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      ClientInfo{Name: name, Version: version},
		Capabilities:    map[string]any{},
	})
	if err != nil {
		return nil, err
	}
	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
	}
	c.server = result.ServerInfo

	// Acknowledge — required by spec; many servers wait for this notification
	// before serving tools/list.
	if err := c.conn.Notify("notifications/initialized", nil); err != nil {
		return &result, fmt.Errorf("notify initialized: %w", err)
	}
	return &result, nil
}

// ListTools queries `tools/list`.
func (c *Client) ListTools() ([]ToolDef, error) {
	resp, err := c.conn.Call("tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var result ToolsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

// CallTool invokes one tool by name.
func (c *Client) CallTool(name string, args json.RawMessage) (*ToolsCallResult, error) {
	resp, err := c.conn.Call("tools/call", ToolsCallParams{Name: name, Arguments: args})
	if err != nil {
		return nil, err
	}
	var result ToolsCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// NewClient wraps an already-running Conn.
func NewClient(conn *Conn) *Client { return &Client{conn: conn} }
