package s04

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAndApplyAdd(t *testing.T) {
	dir := t.TempDir()
	patch := `*** Begin Patch
*** Add File: hello.txt
+hello
+world
*** End Patch
`
	p, err := Parse(patch)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Apply(dir, p); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "hello.txt"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hello\nworld\n" {
		t.Errorf("got %q want %q", string(got), "hello\nworld\n")
	}
}

func TestParseAndApplyDelete(t *testing.T) {
	dir := t.TempDir()
	tgt := filepath.Join(dir, "go.txt")
	_ = os.WriteFile(tgt, []byte("bye\n"), 0o644)

	patch := "*** Begin Patch\n*** Delete File: go.txt\n*** End Patch\n"
	p, err := Parse(patch)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Apply(dir, p); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Stat(tgt); !os.IsNotExist(err) {
		t.Errorf("file still exists: err=%v", err)
	}
}

func TestParseAndApplyUpdate(t *testing.T) {
	dir := t.TempDir()
	tgt := filepath.Join(dir, "code.go")
	original := `package x

func A() int { return 1 }
func B() int { return 2 }
`
	_ = os.WriteFile(tgt, []byte(original), 0o644)

	patch := `*** Begin Patch
*** Update File: code.go
@@ func A
 func A() int { return 1 }
-func B() int { return 2 }
+func B() int { return 3 }
*** End Patch
`
	p, err := Parse(patch)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Apply(dir, p); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, _ := os.ReadFile(tgt)
	if !strings.Contains(string(got), "return 3") {
		t.Errorf("update did not take effect: %q", string(got))
	}
	if strings.Contains(string(got), "return 2") {
		t.Errorf("old line still present")
	}
}

func TestParseAndApplyMove(t *testing.T) {
	dir := t.TempDir()
	tgt := filepath.Join(dir, "old.txt")
	_ = os.WriteFile(tgt, []byte("hi\n"), 0o644)

	patch := `*** Begin Patch
*** Update File: old.txt
*** Move to: new.txt
@@
-hi
+hello
*** End Patch
`
	p, err := Parse(patch)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Apply(dir, p); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Stat(tgt); !os.IsNotExist(err) {
		t.Errorf("old file should have been removed")
	}
	got, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil {
		t.Fatalf("new file missing: %v", err)
	}
	if string(got) != "hello\n" {
		t.Errorf("got %q want %q", string(got), "hello\n")
	}
}

func TestApplyContextMismatch(t *testing.T) {
	dir := t.TempDir()
	tgt := filepath.Join(dir, "x.txt")
	_ = os.WriteFile(tgt, []byte("a\nb\nc\n"), 0o644)

	patch := `*** Begin Patch
*** Update File: x.txt
@@
-z
+y
*** End Patch
`
	p, err := Parse(patch)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Apply(dir, p); err == nil {
		t.Errorf("expected mismatch error, got nil")
	}
}

func TestParseRejectsMalformedHeader(t *testing.T) {
	if _, err := Parse("not a patch"); err == nil {
		t.Errorf("expected header error")
	}
}
