package s04

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Apply writes/deletes/updates files in `root` per the parsed patch.
// All edits are atomic per-file (write to temp then rename).
//
// Returns the list of paths actually modified.
func Apply(root string, p *Patch) ([]string, error) {
	var changed []string
	for _, h := range p.Hunks {
		switch hh := h.(type) {
		case AddFile:
			if err := writeFile(filepath.Join(root, hh.Path), hh.Contents); err != nil {
				return changed, fmt.Errorf("add %s: %w", hh.Path, err)
			}
			changed = append(changed, hh.Path)
		case DeleteFile:
			if err := os.Remove(filepath.Join(root, hh.Path)); err != nil && !os.IsNotExist(err) {
				return changed, fmt.Errorf("delete %s: %w", hh.Path, err)
			}
			changed = append(changed, hh.Path)
		case UpdateFile:
			before, err := os.ReadFile(filepath.Join(root, hh.Path))
			if err != nil {
				return changed, fmt.Errorf("update %s: %w", hh.Path, err)
			}
			after, err := applyChunks(string(before), hh.Chunks)
			if err != nil {
				return changed, fmt.Errorf("update %s: %w", hh.Path, err)
			}
			dst := filepath.Join(root, hh.Path)
			if hh.MovePath != "" {
				dst = filepath.Join(root, hh.MovePath)
			}
			if err := writeFile(dst, after); err != nil {
				return changed, fmt.Errorf("update %s: write: %w", hh.Path, err)
			}
			if hh.MovePath != "" && hh.MovePath != hh.Path {
				if err := os.Remove(filepath.Join(root, hh.Path)); err != nil && !os.IsNotExist(err) {
					return changed, fmt.Errorf("update %s: remove old: %w", hh.Path, err)
				}
				changed = append(changed, hh.Path, hh.MovePath)
			} else {
				changed = append(changed, hh.Path)
			}
		}
	}
	return changed, nil
}

// applyChunks finds each chunk's OldLines exactly in `before` and replaces
// with NewLines. Order matters: each chunk is applied to the result of the
// previous, so chunks must be specified in file order.
func applyChunks(before string, chunks []UpdateChunk) (string, error) {
	cur := splitKeepNewlines(before)
	for ci, ch := range chunks {
		idx := findChunk(cur, ch.OldLines)
		if idx < 0 {
			return "", fmt.Errorf("chunk %d: old lines not found in file (anchor=%q)", ci, ch.ChangeContext)
		}
		cur = append(append(append([]string{}, cur[:idx]...), ch.NewLines...), cur[idx+len(ch.OldLines):]...)
	}
	return strings.Join(cur, "\n"), nil
}

// findChunk returns the index in `lines` where `old` matches exactly, or -1.
func findChunk(lines, old []string) int {
	if len(old) == 0 {
		return 0
	}
outer:
	for i := 0; i+len(old) <= len(lines); i++ {
		for j := range old {
			if lines[i+j] != old[j] {
				continue outer
			}
		}
		return i
	}
	return -1
}

// splitKeepNewlines splits "a\nb\n" into ["a", "b", ""] — that final empty
// string preserves the trailing newline when we Join back.
func splitKeepNewlines(s string) []string {
	return strings.Split(s, "\n")
}

func writeFile(path, contents string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp.learn-codex"
	if err := os.WriteFile(tmp, []byte(contents), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
