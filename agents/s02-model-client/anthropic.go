package s02

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// AnthropicMessages talks to Anthropic's Messages API
// (POST /v1/messages, stream:true). Wire shape differs from OpenAI Chat
// Completions enough that we keep a separate implementation; the Provider
// interface contract is identical, so the rest of learn-codex doesn't care
// which backend is plugged in.
//
// Both APIs are simple SSE streams of JSON deltas; the schema differs.
//   OpenAI CC :  data: {"choices":[{"delta":{"content":"…"}}]}
//   Anthropic :  event: content_block_delta
//                data:  {"delta":{"type":"text_delta","text":"…"}}
type AnthropicMessages struct {
	APIKey     string
	Endpoint   string // default https://api.anthropic.com
	Version    string // default 2023-06-01
	HTTP       *http.Client
}

func NewAnthropic(apiKey string) *AnthropicMessages {
	return &AnthropicMessages{
		APIKey:   apiKey,
		Endpoint: "https://api.anthropic.com",
		Version:  "2023-06-01",
		HTTP:     http.DefaultClient,
	}
}

type anthropicMessage struct {
	Role    string                 `json:"role"`              // user | assistant
	Content []anthropicContentItem `json:"content"`
}

type anthropicContentItem struct {
	Type      string          `json:"type"`                // text | tool_use | tool_result
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`        // tool_use
	Name      string          `json:"name,omitempty"`      // tool_use
	Input     json.RawMessage `json:"input,omitempty"`     // tool_use
	ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
	Stream      bool               `json:"stream"`
	Temperature float64            `json:"temperature,omitempty"`
}

// Stream maps the canonical ProviderRequest to Anthropic's wire shape
// and streams deltas back as ProviderEvents.
func (a *AnthropicMessages) Stream(ctx context.Context, req ProviderRequest, out chan<- ProviderEvent) error {
	body := anthropicRequest{
		Model:       req.Model,
		MaxTokens:   4096,
		Messages:    convertMessagesToAnthropic(req.Messages),
		Tools:       convertToolsToAnthropic(req.Tools),
		Stream:      true,
		Temperature: req.Temperature,
	}
	// Pull out a system message if present (Anthropic models system as a
	// top-level field, not as a message role).
	for _, m := range req.Messages {
		if m.Role == "system" {
			body.System = m.Content
			break
		}
	}

	bts, err := json.Marshal(body)
	if err != nil {
		return err
	}
	hreq, err := http.NewRequestWithContext(ctx, "POST", a.Endpoint+"/v1/messages", bytes.NewReader(bts))
	if err != nil {
		return err
	}
	hreq.Header.Set("content-type", "application/json")
	hreq.Header.Set("x-api-key", a.APIKey)
	hreq.Header.Set("anthropic-version", a.Version)
	hreq.Header.Set("accept", "text/event-stream")

	resp, err := a.HTTP.Do(hreq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("anthropic: status %d: %s", resp.StatusCode, string(b))
	}
	return parseAnthropicSSE(resp.Body, out)
}

// parseAnthropicSSE walks Anthropic's stream:
//
//	event: message_start | content_block_start | content_block_delta |
//	       content_block_stop | message_delta | message_stop
//	data:  {...}
//
// We emit ProvText for text deltas and a ProvToolCall when a tool_use
// content block completes. (Anthropic ships tool args in `partial_json`
// chunks under content_block_delta, similar in spirit to OpenAI's index
// accumulation.)
func parseAnthropicSSE(r io.Reader, out chan<- ProviderEvent) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	type pending struct {
		Index int
		Type  string
		ID    string
		Name  string
		Args  strings.Builder
	}
	blocks := map[int]*pending{}

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		var ev struct {
			Type         string          `json:"type"`
			Index        int             `json:"index"`
			ContentBlock json.RawMessage `json:"content_block"`
			Delta        struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
			StopReason string `json:"stop_reason"`
		}
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "content_block_start":
			var b struct {
				Type, ID, Name string
				Input          json.RawMessage `json:"input"`
			}
			_ = json.Unmarshal(ev.ContentBlock, &b)
			p := &pending{Index: ev.Index, Type: b.Type, ID: b.ID, Name: b.Name}
			blocks[ev.Index] = p
		case "content_block_delta":
			p, ok := blocks[ev.Index]
			if !ok {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				out <- ProvText{Delta: ev.Delta.Text}
			case "input_json_delta":
				p.Args.WriteString(ev.Delta.PartialJSON)
			}
		case "content_block_stop":
			p, ok := blocks[ev.Index]
			if !ok {
				continue
			}
			if p.Type == "tool_use" {
				out <- ProvToolCall{Call: ToolCall{
					ID: p.ID, Type: "function",
					Function: ToolCallFn{Name: p.Name, Args: json.RawMessage(p.Args.String())},
				}}
			}
			delete(blocks, ev.Index)
		case "message_stop":
			out <- ProvDone{FinishReason: "stop"}
			return nil
		}
	}
	return scanner.Err()
}

// convertMessagesToAnthropic skips system messages (handled separately) and
// translates user/assistant/tool roles + tool_calls into Anthropic's
// content-block shape.
func convertMessagesToAnthropic(msgs []ChatMessage) []anthropicMessage {
	var out []anthropicMessage
	for _, m := range msgs {
		switch m.Role {
		case "system":
			continue
		case "user":
			out = append(out, anthropicMessage{Role: "user", Content: []anthropicContentItem{{Type: "text", Text: m.Content}}})
		case "assistant":
			items := []anthropicContentItem{}
			if m.Content != "" {
				items = append(items, anthropicContentItem{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				items = append(items, anthropicContentItem{
					Type: "tool_use", ID: tc.ID, Name: tc.Function.Name, Input: tc.Function.Args,
				})
			}
			out = append(out, anthropicMessage{Role: "assistant", Content: items})
		case "tool":
			out = append(out, anthropicMessage{Role: "user", Content: []anthropicContentItem{
				{Type: "tool_result", ToolUseID: m.ToolCallID, Text: m.Content},
			}})
		}
	}
	return out
}

func convertToolsToAnthropic(tools []ToolSchema) []anthropicTool {
	var out []anthropicTool
	for _, t := range tools {
		out = append(out, anthropicTool{
			Name: t.Function.Name, Description: t.Function.Description, InputSchema: t.Function.Parameters,
		})
	}
	return out
}
