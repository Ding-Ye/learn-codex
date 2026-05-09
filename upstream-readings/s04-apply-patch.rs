// =============================================================================
//  Upstream reading for s04 — apply-patch (V4A DSL)
//  Source: codex-rs/apply-patch/src/lib.rs
//          codex-rs/apply-patch/src/parser.rs (conceptual; merged into lib in upstream)
// =============================================================================

/// Source: codex-rs/apply-patch/src/lib.rs
pub enum Hunk {
    AddFile {
        path: PathBuf,
        contents: String,
    },
    DeleteFile {
        path: PathBuf,
    },
    UpdateFile {
        path: PathBuf,
        move_path: Option<PathBuf>,
        chunks: Vec<UpdateFileChunk>,
    },
}

/// Source: codex-rs/apply-patch/src/lib.rs
pub struct UpdateFileChunk {
    pub old_lines: Vec<String>,
    pub new_lines: Vec<String>,
    pub change_context: Option<String>,    // the "@@ ..." anchor (optional)
    pub is_end_of_file: bool,
}

/// Source: codex-rs/apply-patch/src/lib.rs
pub fn parse_patch(text: &str) -> Result<Vec<Hunk>, ParseError> {
    // Tokens we recognise:
    //   *** Begin Patch
    //   *** Add File: <path>
    //   *** Delete File: <path>
    //   *** Update File: <path>
    //   *** Move to: <path>
    //   @@ [optional context anchor]
    //   *** End of File
    //   *** End Patch
    //
    // For Update File: each chunk body is a run of " " | "-" | "+" prefixed
    // lines until the next @@ or *** marker. context lines (" ") go into
    // BOTH old_lines and new_lines. "-" lines into old_lines only. "+" lines
    // into new_lines only.
}

/// Source: codex-rs/apply-patch/src/lib.rs
pub fn apply_patch(
    cwd: &Path,
    hunks: Vec<Hunk>,
    fs: &mut impl Filesystem,
) -> Result<AppliedPatchDelta, ApplyError> {
    let mut created = Vec::new();
    let mut deleted = Vec::new();
    let mut modified = Vec::new();

    for hunk in hunks {
        match hunk {
            Hunk::AddFile { path, contents } => {
                fs.write_atomic(&cwd.join(&path), contents.as_bytes())?;
                created.push(path);
            }
            Hunk::DeleteFile { path } => {
                fs.remove(&cwd.join(&path))?;
                deleted.push(path);
            }
            Hunk::UpdateFile { path, move_path, chunks } => {
                let mut text = fs.read_to_string(&cwd.join(&path))?;
                for ch in chunks {
                    text = apply_chunk(&text, &ch)?;       // ★ exact match
                }
                let dst = move_path.as_ref().unwrap_or(&path);
                fs.write_atomic(&cwd.join(dst), text.as_bytes())?;
                if move_path.is_some() && move_path.as_ref() != Some(&path) {
                    fs.remove(&cwd.join(&path))?;
                }
                modified.push(path);
            }
        }
    }
    Ok(AppliedPatchDelta { created, deleted, modified })
}

/// AppliedPatchDelta — what `apply_patch` reports back to the rollout
/// recorder and the TUI diff view. learn-codex returns []string instead.
pub struct AppliedPatchDelta {
    pub created: Vec<PathBuf>,
    pub deleted: Vec<PathBuf>,
    pub modified: Vec<PathBuf>,
}

// =============================================================================
// Comparison summary
//
//   Concept            | Upstream                       | learn-codex (s04)
//   -------------------+--------------------------------+-----------------------------
//   wire format        | V4A DSL (this file)            | identical syntax (Parse)
//   FS abstraction     | trait Filesystem (mockable)    | os.* directly
//   atomic write       | write tmp + fsync + rename     | write tmp + rename (no fsync)
//   delta return       | AppliedPatchDelta              | []string changed paths
//   end-of-file marker | is_end_of_file flag honoured   | flag carried but unused
// =============================================================================
