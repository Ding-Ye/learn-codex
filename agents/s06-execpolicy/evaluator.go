package s06

// Classify returns the first rule that matches `command`, or nil + the default
// decision when nothing matches. Order in the file matters; rules are
// evaluated top-to-bottom, first-match-wins.
//
// To get longest-prefix-wins instead, sort rules by len(Tokens) DESC before
// passing them to Classify. We deliberately don't auto-sort so the file
// remains readable as-written.
func (p *Policy) Classify(command []string) (Decision, *Rule) {
	for i := range p.Rules {
		r := &p.Rules[i]
		if r.matches(command) {
			return r.Decision, r
		}
	}
	return p.Default, nil
}

func (r *Rule) matches(cmd []string) bool {
	switch r.Match {
	case MatchExact:
		if len(cmd) != len(r.Tokens) {
			return false
		}
		for i := range cmd {
			if cmd[i] != r.Tokens[i] {
				return false
			}
		}
		return true
	case MatchPrefix:
		if len(cmd) < len(r.Tokens) {
			return false
		}
		for i := range r.Tokens {
			if cmd[i] != r.Tokens[i] {
				return false
			}
		}
		return true
	}
	return false
}
