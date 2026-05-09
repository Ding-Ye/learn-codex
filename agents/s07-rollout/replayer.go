package s07

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// Replay reads `path` line by line and returns:
//   - the meta line (always at index 0; nil if file empty or missing meta),
//   - the rest of the items in original order.
//
// Truncated last lines (mid-write crash) are skipped with a warning to
// stderr; we don't fail the whole replay.
func Replay(path string) (*SessionMeta, []RolloutItem, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	var meta *SessionMeta
	var items []RolloutItem
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var item RolloutItem
		if err := json.Unmarshal(raw, &item); err != nil {
			fmt.Fprintf(os.Stderr, "rollout: skipping bad line %d: %v\n", lineNo, err)
			continue
		}
		if item.Kind == KindSessionMeta && meta == nil {
			var m SessionMeta
			if err := json.Unmarshal(item.Payload, &m); err != nil {
				return nil, nil, fmt.Errorf("decode meta on line %d: %w", lineNo, err)
			}
			meta = &m
			continue
		}
		items = append(items, item)
	}
	if err := scanner.Err(); err != nil {
		// scanner err on a torn last line ≈ truncation; we keep what we have
		fmt.Fprintf(os.Stderr, "rollout: scan err (treating as truncation): %v\n", err)
	}
	return meta, items, nil
}

// Reconstruct extracts the conversation history (user/assistant text + tool
// calls + tool results) in chronological order, returning a flat list of
// (role, payload) tuples that the agent loop can splice back into the
// model's `messages` array on resume.
type ResumedMessage struct {
	Role string // "user" | "assistant" | "tool"
	Item RolloutItem
}

func Reconstruct(items []RolloutItem) []ResumedMessage {
	var out []ResumedMessage
	for _, it := range items {
		switch it.Kind {
		case KindUserInput:
			out = append(out, ResumedMessage{Role: "user", Item: it})
		case KindAssistantText, KindAssistantToolCall:
			out = append(out, ResumedMessage{Role: "assistant", Item: it})
		case KindToolResult:
			out = append(out, ResumedMessage{Role: "tool", Item: it})
			// KindEvent and others are dropped from chat reconstruction.
		}
	}
	return out
}
