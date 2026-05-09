package s02

import (
	"context"
	"encoding/json"
	"fmt"
)

// ChatMessage is the OpenAI Chat Completions message shape.
//
// We use the OpenAI Chat Completions schema instead of the Responses API that
// upstream codex actually uses. Reasons:
//   - Chat Completions is the lingua franca every learner already knows.
//   - The Responses API ships ResponseItem objects (text/tool_use/reasoning/…)
//     which obscure the lesson.
//   - Codex's `provider abstraction` (codex-rs/model-provider/) hides this
//     same difference internally.
type ChatMessage struct {
	Role       string     `json:"role"`               // "system" | "user" | "assistant" | "tool"
	Content    string     `json:"content,omitempty"`  //
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"` // assistant -> tool_use
	ToolCallID string     `json:"tool_call_id,omitempty"` // tool -> result
	Name       string     `json:"name,omitempty"`
}

// ToolCall is one model-emitted call: the function name + JSON arguments.
type ToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"` // always "function" for chat-completions
	Function ToolCallFn      `json:"function"`
}

type ToolCallFn struct {
	Name     string          `json:"name"`
	Args     json.RawMessage `json:"arguments,omitempty"` // string-encoded JSON
}

// ToolSchema is the OpenAI tools[].function shape — what we send to the
// model so it knows what tools exist.
type ToolSchema struct {
	Type     string             `json:"type"` // "function"
	Function ToolSchemaFunction `json:"function"`
}

type ToolSchemaFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema
}

// ProviderRequest is the input to a streaming completion.
type ProviderRequest struct {
	Model       string
	Messages    []ChatMessage
	Tools       []ToolSchema
	Temperature float64 // 0 == deterministic
}

// ProviderEvent is the streamed output. Tagged union via marker method.
type ProviderEvent interface{ isProvEvent() }

type ProvText struct{ Delta string }
type ProvToolCall struct{ Call ToolCall }
type ProvDone struct{ FinishReason string }
type ProvError struct{ Err error }

func (ProvText) isProvEvent()     {}
func (ProvToolCall) isProvEvent() {}
func (ProvDone) isProvEvent()     {}
func (ProvError) isProvEvent()    {}

// Provider is the abstraction every later session uses. The default impl is
// OpenAIChatCompletions (in openai.go); Phase G adds Anthropic Messages and
// a deterministic echo provider.
type Provider interface {
	Stream(ctx context.Context, req ProviderRequest, out chan<- ProviderEvent) error
}

// helper: stringify a ToolCall for logs.
func (t ToolCall) String() string {
	return fmt.Sprintf("%s(%s)", t.Function.Name, string(t.Function.Args))
}
