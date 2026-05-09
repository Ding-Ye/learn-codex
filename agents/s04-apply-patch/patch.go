package s04

// Hunk is one of three operations in a V4A patch. Tagged union.
type Hunk interface{ isHunk() }

// AddFile creates a new file with literal contents.
type AddFile struct {
	Path     string
	Contents string
}

// DeleteFile removes an existing file.
type DeleteFile struct {
	Path string
}

// UpdateFile mutates an existing file. MovePath, if non-empty, renames the
// file after the chunks are applied. Chunks must be applied in order.
type UpdateFile struct {
	Path     string
	MovePath string // optional rename target (`*** Move to:` directive)
	Chunks   []UpdateChunk
}

// UpdateChunk replaces OldLines with NewLines, optionally anchored by a
// ChangeContext line ("@@ <context>"), at the (first) location where
// OldLines + leading context match the file content.
type UpdateChunk struct {
	ChangeContext string   // optional context line that anchors the chunk
	OldLines      []string // lines to remove (the "-" lines)
	NewLines      []string // lines to insert (the "+" lines)
	IsEOF         bool     // true if the chunk ends at end-of-file (no trailing context)
}

func (AddFile) isHunk()    {}
func (DeleteFile) isHunk() {}
func (UpdateFile) isHunk() {}

// Patch is a parsed *** Begin Patch ... *** End Patch block.
type Patch struct {
	Hunks []Hunk
}
