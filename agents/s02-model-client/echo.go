package s02

import (
	"context"
	"fmt"
)

// EchoProvider is a deterministic Provider used in tests and multi-model
// demos. It scripts a sequence of `ProviderEvent`s and replays them on
// every Stream call.
//
// This is the minimal Provider impl that demonstrates "any LLM (or no LLM
// at all) can plug into the agent loop, as long as it satisfies the same
// interface".
type EchoProvider struct {
	Script []ProviderEvent
	Fail   error
}

// Stream emits each scripted event then closes via finish. Honours ctx.
func (e *EchoProvider) Stream(ctx context.Context, _ ProviderRequest, out chan<- ProviderEvent) error {
	if e.Fail != nil {
		return e.Fail
	}
	for _, ev := range e.Script {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- ev:
		}
	}
	out <- ProvDone{FinishReason: "stop"}
	return nil
}

// EchoText is sugar for "produce one text reply word-by-word".
func EchoText(text string) *EchoProvider {
	return &EchoProvider{
		Script: []ProviderEvent{ProvText{Delta: text}},
	}
}

// EchoToolCall is sugar for "produce one tool call".
func EchoToolCall(id, name, argsJSON string) *EchoProvider {
	return &EchoProvider{
		Script: []ProviderEvent{ProvToolCall{Call: ToolCall{
			ID: id, Type: "function",
			Function: ToolCallFn{Name: name, Args: []byte(argsJSON)},
		}}},
	}
}

// String for debug logging.
func (e *EchoProvider) String() string { return fmt.Sprintf("echo(%d events)", len(e.Script)) }
