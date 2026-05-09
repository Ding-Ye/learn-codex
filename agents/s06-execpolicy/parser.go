package s06

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Parse reads a tiny .rules-style DSL:
//
//	# comment line
//	rule allow  prefix "git status"
//	rule prompt prefix "rm"           reason "destructive"
//	rule forbid prefix "sudo"         reason "privilege escalation"
//	rule allow  exact  "go test ./..."
//	default prompt
//
// Whitespace separates fields. The pattern is a single double-quoted string;
// it gets shell-style tokenised on whitespace inside (no quoting nesting; the
// goal is teaching, not bash compatibility).
func Parse(r io.Reader) (*Policy, error) {
	p := &Policy{Default: DecidePrompt}
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields, err := splitFields(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		switch fields[0] {
		case "default":
			if len(fields) != 2 {
				return nil, fmt.Errorf("line %d: 'default' takes one arg", lineNo)
			}
			d, err := parseDecision(fields[1])
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
			p.Default = d
		case "rule":
			if len(fields) < 4 {
				return nil, fmt.Errorf("line %d: rule needs at least: rule <decision> <match> <pattern>", lineNo)
			}
			d, err := parseDecision(fields[1])
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
			m, err := parseMatch(fields[2])
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
			pat := fields[3]
			r := Rule{Decision: d, Match: m, Tokens: strings.Fields(pat)}
			// optional `reason "..."` at the end
			if len(fields) >= 6 && fields[4] == "reason" {
				r.Reason = fields[5]
			}
			p.Rules = append(p.Rules, r)
		default:
			return nil, fmt.Errorf("line %d: unknown directive %q", lineNo, fields[0])
		}
	}
	return p, scanner.Err()
}

func parseDecision(s string) (Decision, error) {
	switch s {
	case "allow":
		return DecideAllow, nil
	case "prompt":
		return DecidePrompt, nil
	case "forbid", "forbidden":
		return DecideForbidden, nil
	}
	return 0, fmt.Errorf("unknown decision %q", s)
}

func parseMatch(s string) (Match, error) {
	switch s {
	case "prefix":
		return MatchPrefix, nil
	case "exact":
		return MatchExact, nil
	}
	return 0, fmt.Errorf("unknown match %q", s)
}

// splitFields splits on whitespace except inside "..." quotes.
// Returns slices with quoted strings already unquoted.
func splitFields(line string) ([]string, error) {
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, r := range line {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ' ' || r == '\t':
			if inQuote {
				cur.WriteRune(r)
			} else if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if inQuote {
		return nil, fmt.Errorf("unterminated quote")
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out, nil
}
