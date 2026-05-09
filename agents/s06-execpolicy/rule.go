package s06

// Decision is the verdict of evaluating a command against a Policy.
type Decision int

const (
	DecideAllow     Decision = iota // skip approval, run
	DecidePrompt                    // ask the user (s05 gate)
	DecideForbidden                 // never run
)

func (d Decision) String() string {
	switch d {
	case DecideAllow:
		return "allow"
	case DecidePrompt:
		return "prompt"
	case DecideForbidden:
		return "forbid"
	}
	return "?"
}

// Match describes how a Rule matches the command:
//
//	prefix  — first N elements of the command equal this rule's tokens
//	exact   — the entire command equals this rule's tokens
type Match int

const (
	MatchPrefix Match = iota
	MatchExact
)

// Rule is one line of the policy.
type Rule struct {
	Decision Decision
	Match    Match
	Tokens   []string // pre-tokenised pattern
	Reason   string   // optional human-readable justification
}

// Policy is an ordered list of rules. First match wins.
type Policy struct {
	Rules    []Rule
	Default  Decision // returned when no rule matches; default DecidePrompt
}
