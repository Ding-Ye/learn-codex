package s01

// Op is what the frontend sends *into* the agent. It's a tagged union: each
// concrete type implements isOp() so the type switch is exhaustive at the
// receiver. Codex's upstream Rust enum has ~20 variants; we keep the four that
// reveal the protocol shape.
type Op interface{ isOp() }

// OpUserInput — the user typed something at the prompt.
type OpUserInput struct{ Text string }

// OpExecApproval — used in s05 onwards. Defined here because the protocol
// shape is fixed in s01.
type OpExecApproval struct {
	ID       string
	Approved bool
}

// OpInterrupt — cancel the current turn. The loop should stop emitting
// AgentMessage events and produce a TurnComplete with cancelled=true.
type OpInterrupt struct{}

// OpShutdown — close the event channel and stop the goroutine.
type OpShutdown struct{}

func (OpUserInput) isOp()    {}
func (OpExecApproval) isOp() {}
func (OpInterrupt) isOp()    {}
func (OpShutdown) isOp()     {}

// Submission wraps an Op with an id. The id is what subsequent EventMsgs
// reference so the frontend can correlate "request → its events".
type Submission struct {
	ID string
	Op Op
}
