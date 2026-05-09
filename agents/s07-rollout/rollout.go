package s07

import "encoding/json"

// RolloutItem is one line in a session JSONL file.
//
// Why one tagged union per line instead of one struct per kind?
// - Append-only files love uniform line shape.
// - `jq` and grep stay useful: filter by `.kind`.
// - The reader doesn't need to know all kinds at compile time; unknowns are
//   skipped with a warning.
type RolloutItem struct {
	Kind    string          `json:"kind"`
	TS      int64           `json:"ts"` // unix-ms
	Payload json.RawMessage `json:"payload"`
}

// Known kinds in learn-codex (extend freely; unknown kinds round-trip):
const (
	KindSessionMeta     = "session_meta"
	KindUserInput       = "user_input"
	KindAssistantText   = "assistant_text"
	KindAssistantToolCall = "assistant_tool_call"
	KindToolResult      = "tool_result"
	KindEvent           = "event"
)

// SessionMeta is the first line of every rollout file.
type SessionMeta struct {
	ID        string            `json:"id"`
	StartedAt int64             `json:"started_at"`
	Model     string            `json:"model"`
	Cwd       string            `json:"cwd"`
	Extras    map[string]string `json:"extras,omitempty"`
}

// UserInputItem records `OpUserInput`.
type UserInputItem struct{ Text string `json:"text"` }

// AssistantTextItem records what the model said.
type AssistantTextItem struct{ Text string `json:"text"` }

// AssistantToolCallItem records a tool the model asked for.
type AssistantToolCallItem struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// ToolResultItem records the result we fed back into the next turn.
type ToolResultItem struct {
	ID       string `json:"id"`
	Output   string `json:"output"`
	ExitCode int    `json:"exit_code,omitempty"`
}

// EventItem captures any other EvMsg verbatim — useful for replaying TUI
// events for reasons beyond chat reconstruction.
type EventItem struct {
	Kind    string          `json:"event_kind"`
	Payload json.RawMessage `json:"payload"`
}
