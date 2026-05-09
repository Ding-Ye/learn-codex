package s04

import (
	"bufio"
	"fmt"
	"strings"
)

// Parse reads a V4A patch text and returns its Hunks.
//
// Grammar (simplified — matches the subset codex-rs/apply-patch supports):
//
//	*** Begin Patch
//	(file-block)+
//	*** End Patch
//
// file-block is one of:
//
//	*** Add File: <path>
//	+<line>
//	+<line>
//	...
//
//	*** Delete File: <path>
//
//	*** Update File: <path>
//	[*** Move to: <new-path>]
//	(@@ chunk-block)+
//
// chunk-block:
//
//	@@ [optional context anchor]
//	(<space-prefixed context line>)*
//	(-<old line> | +<new line>)+
//	(<space-prefixed context line>)*
//
// We don't enforce trailing context.
func Parse(patchText string) (*Patch, error) {
	scanner := bufio.NewScanner(strings.NewReader(patchText))
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty patch")
	}
	if strings.TrimSpace(lines[0]) != "*** Begin Patch" {
		return nil, fmt.Errorf("missing '*** Begin Patch' header (got %q)", lines[0])
	}

	p := &Patch{}
	i := 1
	for i < len(lines) {
		line := lines[i]
		switch {
		case line == "*** End Patch":
			return p, nil
		case strings.HasPrefix(line, "*** Add File: "):
			path := strings.TrimPrefix(line, "*** Add File: ")
			i++
			var content strings.Builder
			for i < len(lines) && !isFileHeader(lines[i]) {
				if strings.HasPrefix(lines[i], "+") {
					content.WriteString(lines[i][1:])
					content.WriteByte('\n')
				}
				// blank lines and other prefixes ignored inside Add File
				i++
			}
			p.Hunks = append(p.Hunks, AddFile{Path: path, Contents: content.String()})
		case strings.HasPrefix(line, "*** Delete File: "):
			path := strings.TrimPrefix(line, "*** Delete File: ")
			p.Hunks = append(p.Hunks, DeleteFile{Path: path})
			i++
		case strings.HasPrefix(line, "*** Update File: "):
			path := strings.TrimPrefix(line, "*** Update File: ")
			i++
			move := ""
			if i < len(lines) && strings.HasPrefix(lines[i], "*** Move to: ") {
				move = strings.TrimPrefix(lines[i], "*** Move to: ")
				i++
			}
			var chunks []UpdateChunk
			for i < len(lines) && !isFileHeader(lines[i]) {
				if !strings.HasPrefix(lines[i], "@@") {
					return nil, fmt.Errorf("update file: expected @@ or file header at line %d (got %q)", i+1, lines[i])
				}
				ctx := strings.TrimSpace(strings.TrimPrefix(lines[i], "@@"))
				i++
				ch, ni, err := parseChunk(lines, i, ctx)
				if err != nil {
					return nil, err
				}
				chunks = append(chunks, ch)
				i = ni
			}
			p.Hunks = append(p.Hunks, UpdateFile{Path: path, MovePath: move, Chunks: chunks})
		default:
			return nil, fmt.Errorf("unexpected line %d: %q", i+1, line)
		}
	}
	return nil, fmt.Errorf("missing '*** End Patch' footer")
}

func isFileHeader(line string) bool {
	return line == "*** End Patch" ||
		strings.HasPrefix(line, "*** Add File: ") ||
		strings.HasPrefix(line, "*** Delete File: ") ||
		strings.HasPrefix(line, "*** Update File: ")
}

// parseChunk consumes one chunk body starting at lines[i], returning the
// parsed UpdateChunk and the index of the next un-consumed line.
//
// Accepts mixed runs of " ", "-", "+" lines until the next "@@" / file header.
// We don't separate context-before/context-after; they're merged into
// OldLines (context+old) and NewLines (context+new).
func parseChunk(lines []string, start int, ctx string) (UpdateChunk, int, error) {
	ch := UpdateChunk{ChangeContext: ctx}
	i := start
	for i < len(lines) {
		l := lines[i]
		if strings.HasPrefix(l, "@@") || isFileHeader(l) {
			break
		}
		switch {
		case strings.HasPrefix(l, " "):
			body := l[1:]
			ch.OldLines = append(ch.OldLines, body)
			ch.NewLines = append(ch.NewLines, body)
		case strings.HasPrefix(l, "-"):
			ch.OldLines = append(ch.OldLines, l[1:])
		case strings.HasPrefix(l, "+"):
			ch.NewLines = append(ch.NewLines, l[1:])
		case l == "*** End of File":
			ch.IsEOF = true
		case l == "":
			// blank separator; treat as a context blank line
			ch.OldLines = append(ch.OldLines, "")
			ch.NewLines = append(ch.NewLines, "")
		default:
			return ch, i, fmt.Errorf("chunk: unrecognised line %d: %q", i+1, l)
		}
		i++
	}
	if len(ch.OldLines) == 0 && len(ch.NewLines) == 0 {
		return ch, i, fmt.Errorf("empty chunk near line %d", start+1)
	}
	return ch, i, nil
}
