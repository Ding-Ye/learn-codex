package s10

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestRegistryDispatchUnknown(t *testing.T) {
	r := NewRegistry()
	r.Add(Subcommand{Name: "exec", Synopsis: "one-shot", Run: func([]string, io.Reader, io.Writer, io.Writer) int { return 0 }})
	var stderr bytes.Buffer
	code := r.Dispatch([]string{"nope"}, nil, io.Discard, &stderr)
	if code != 2 {
		t.Errorf("code=%d want 2 (unknown)", code)
	}
	if !strings.Contains(stderr.String(), "unknown") {
		t.Errorf("stderr=%q lacks 'unknown'", stderr.String())
	}
}

func TestRegistryDispatchHelp(t *testing.T) {
	r := NewRegistry()
	r.Add(Subcommand{Name: "exec", Synopsis: "one-shot", Run: func([]string, io.Reader, io.Writer, io.Writer) int { return 0 }})
	var stderr bytes.Buffer
	if code := r.Dispatch([]string{"help"}, nil, io.Discard, &stderr); code != 0 {
		t.Errorf("code=%d want 0", code)
	}
	if !strings.Contains(stderr.String(), "exec") {
		t.Errorf("help missing exec: %q", stderr.String())
	}
}

func TestRegistryDispatchExec(t *testing.T) {
	called := false
	r := NewRegistry()
	r.Add(Subcommand{
		Name: "exec",
		Run: func(args []string, _ io.Reader, _ io.Writer, _ io.Writer) int {
			called = true
			if len(args) != 1 || args[0] != "hello" {
				t.Errorf("args=%v want [hello]", args)
			}
			return 7
		},
	})
	if code := r.Dispatch([]string{"exec", "hello"}, nil, io.Discard, io.Discard); code != 7 {
		t.Errorf("code=%d want 7", code)
	}
	if !called {
		t.Errorf("subcommand never ran")
	}
}

func TestSlashHelpListsCommands(t *testing.T) {
	s := DefaultSet()
	var out bytes.Buffer
	handled, quit := s.Match("/help", &out)
	if !handled || quit {
		t.Errorf("handled=%v quit=%v want true,false", handled, quit)
	}
	if !strings.Contains(out.String(), "/help") {
		t.Errorf("output missing /help: %q", out.String())
	}
}

func TestSlashQuit(t *testing.T) {
	s := DefaultSet()
	_, quit := s.Match("/quit", io.Discard)
	if !quit {
		t.Errorf("/quit didn't trigger quit")
	}
}

func TestSlashUnknown(t *testing.T) {
	s := DefaultSet()
	var out bytes.Buffer
	handled, quit := s.Match("/nope", &out)
	if !handled {
		t.Errorf("expected handled=true even for unknown slash command")
	}
	if quit {
		t.Errorf("unknown slash should not quit")
	}
	if !strings.Contains(out.String(), "unknown") {
		t.Errorf("output: %q", out.String())
	}
}

func TestSlashIgnoresPlainText(t *testing.T) {
	s := DefaultSet()
	handled, _ := s.Match("hello world", io.Discard)
	if handled {
		t.Errorf("non-slash text incorrectly handled as slash command")
	}
}
