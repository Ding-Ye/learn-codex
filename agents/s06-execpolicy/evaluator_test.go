package s06

import (
	"strings"
	"testing"
)

const sample = `
# default policy
default prompt

rule allow  prefix "git status"
rule allow  prefix "ls"
rule prompt prefix "rm"  reason "destructive"
rule forbid prefix "sudo" reason "privilege escalation"
rule allow  exact  "go test ./..."
`

func mustParse(t *testing.T, src string) *Policy {
	t.Helper()
	p, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return p
}

func TestClassifyAllowPrefix(t *testing.T) {
	p := mustParse(t, sample)
	d, r := p.Classify([]string{"git", "status"})
	if d != DecideAllow {
		t.Errorf("decision=%v want allow", d)
	}
	if r == nil {
		t.Errorf("rule=nil")
	}
}

func TestClassifyForbid(t *testing.T) {
	p := mustParse(t, sample)
	d, _ := p.Classify([]string{"sudo", "ls"})
	if d != DecideForbidden {
		t.Errorf("decision=%v want forbid", d)
	}
}

func TestClassifyPromptDestructive(t *testing.T) {
	p := mustParse(t, sample)
	d, r := p.Classify([]string{"rm", "-rf", "/tmp"})
	if d != DecidePrompt {
		t.Errorf("decision=%v want prompt", d)
	}
	if r == nil || r.Reason != "destructive" {
		t.Errorf("reason=%q want destructive", r.Reason)
	}
}

func TestClassifyExactMatch(t *testing.T) {
	p := mustParse(t, sample)
	d, _ := p.Classify([]string{"go", "test", "./..."})
	if d != DecideAllow {
		t.Errorf("decision=%v want allow", d)
	}
	d2, _ := p.Classify([]string{"go", "test", "./pkg/..."})
	// exact rule shouldn't match longer suffix
	if d2 == DecideAllow {
		t.Errorf("exact match should not allow longer command")
	}
}

func TestDefaultWhenNoRuleMatches(t *testing.T) {
	p := mustParse(t, sample)
	d, r := p.Classify([]string{"echo", "hi"})
	if r != nil {
		t.Errorf("rule=%v want nil (fallback)", r)
	}
	if d != DecidePrompt {
		t.Errorf("default decision=%v want prompt", d)
	}
}

func TestParserRejectsMalformed(t *testing.T) {
	if _, err := Parse(strings.NewReader(`rule unknown prefix "x"`)); err == nil {
		t.Errorf("expected error for unknown decision")
	}
	if _, err := Parse(strings.NewReader(`rule allow exact "unterminated`)); err == nil {
		t.Errorf("expected error for unterminated quote")
	}
}
