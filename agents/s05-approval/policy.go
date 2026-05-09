package s05

// ApprovalPolicy decides when a tool call requires explicit user confirmation
// before it can run. Mirror of codex-rs/protocol's AskForApproval enum.
type ApprovalPolicy int

const (
	// ApprovalNever — never ask. Useful for `codex exec --auto-approve`.
	ApprovalNever ApprovalPolicy = iota

	// ApprovalOnFailure — first attempt without asking; on non-zero exit, ask.
	// (Not modelled in s05 — it requires retry context; documented for completeness.)
	ApprovalOnFailure

	// ApprovalOnRequest — every command requires approval. Default.
	ApprovalOnRequest

	// ApprovalUnlessTrusted — ask except for an allowlist of "known safe"
	// commands (s06's execpolicy will populate this list).
	ApprovalUnlessTrusted

	// ApprovalGranular — like UnlessTrusted but with per-rule reasons.
	// (Surfaced as a string in the request payload.)
	ApprovalGranular
)

func (a ApprovalPolicy) String() string {
	switch a {
	case ApprovalNever:
		return "Never"
	case ApprovalOnFailure:
		return "OnFailure"
	case ApprovalOnRequest:
		return "OnRequest"
	case ApprovalUnlessTrusted:
		return "UnlessTrusted"
	case ApprovalGranular:
		return "Granular"
	}
	return "?"
}
