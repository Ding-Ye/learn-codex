package s01

// EventMsg is what the agent emits *out* to the frontend. Tagged union via
// isEventMsg(). Upstream Codex has ~30 variants; we keep the 6 that reveal
// the turn lifecycle.
type EventMsg interface{ isEventMsg() }

// EvTurnStarted — emitted as soon as a UserInput op is dequeued.
type EvTurnStarted struct{ TurnID string }

// EvAgentMessage — assistant text, possibly delivered as multiple chunks per
// turn. In s01 we emit one chunk; in s02 we'll stream from the LLM.
type EvAgentMessage struct {
	TurnID string
	Text   string
}

// EvExecApprovalRequest — backend asks the frontend to approve a tool call.
// Used in s05 onwards; defined here because the protocol shape is locked in s01.
type EvExecApprovalRequest struct {
	ID      string
	Command []string
	Reason  string
}

// EvTurnComplete — turn finished; Cancelled=true if it ended via OpInterrupt.
type EvTurnComplete struct {
	TurnID    string
	Cancelled bool
}

// EvError — anything that prevents the turn from completing normally.
type EvError struct {
	TurnID  string
	Message string
}

// EvShutdownComplete — last event before the channel closes.
type EvShutdownComplete struct{}

func (EvTurnStarted) isEventMsg()         {}
func (EvAgentMessage) isEventMsg()        {}
func (EvExecApprovalRequest) isEventMsg() {}
func (EvTurnComplete) isEventMsg()        {}
func (EvError) isEventMsg()               {}
func (EvShutdownComplete) isEventMsg()    {}
