package applypatch

import (
	"errors"
	"fmt"
	"io"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// ApplyPatch parses patch and applies it to the filesystem rooted at cwd,
// writing a success summary to stdout or an error message to stderr. It returns
// the committed [AppliedPatchDelta] on success, or an [*ApplyFailure] (which
// carries the partial delta) on failure. It is a port of Rust `apply_patch`.
//
// fsys is the filesystem to operate on; pass [OSFileSystem]{} for the real
// filesystem.
func ApplyPatch(patch string, cwd abspath.AbsolutePathBuf, stdout, stderr io.Writer, fsys FileSystem) (AppliedPatchDelta, error) {
	result, err := ParsePatch(patch)
	if err != nil {
		var pe *ParseError
		if errors.As(err, &pe) {
			switch pe.Kind {
			case InvalidHunk:
				if _, werr := fmt.Fprintf(stderr, "Invalid patch hunk on line %d: %s\n", pe.LineNumber, pe.Message); werr != nil {
					return AppliedPatchDelta{}, applyFailureWithoutDelta(wrapIOError(werr))
				}
			default:
				if _, werr := fmt.Fprintf(stderr, "Invalid patch: %s\n", pe.Message); werr != nil {
					return AppliedPatchDelta{}, applyFailureWithoutDelta(wrapIOError(werr))
				}
			}
		}
		return AppliedPatchDelta{}, applyFailureWithoutDelta(err)
	}

	return ApplyHunks(result.Hunks, cwd, stdout, stderr, fsys)
}

// ApplyHunks applies already-parsed hunks, updating stdout/stderr. It is a port
// of Rust `apply_hunks`.
func ApplyHunks(hunks []Hunk, cwd abspath.AbsolutePathBuf, stdout, stderr io.Writer, fsys FileSystem) (AppliedPatchDelta, error) {
	delta := emptyAppliedPatchDelta()
	affected, applyErr := applyHunksToFiles(hunks, cwd, fsys, &delta)
	if applyErr == nil {
		if err := PrintSummary(affected, stdout); err != nil {
			return AppliedPatchDelta{}, newApplyFailure(wrapIOError(err), delta)
		}
		return delta, nil
	}

	msg := applyErr.Error()
	if _, werr := fmt.Fprintf(stderr, "%s\n", msg); werr != nil {
		return AppliedPatchDelta{}, newApplyFailure(wrapIOError(werr), delta)
	}
	return AppliedPatchDelta{}, newApplyFailure(applyErr, delta)
}

// applyHunksToFiles applies each hunk to the filesystem, tracking the committed
// delta. It is a port of Rust `apply_hunks_to_files`.
func applyHunksToFiles(hunks []Hunk, cwd abspath.AbsolutePathBuf, fsys FileSystem, delta *AppliedPatchDelta) (AffectedPaths, error) {
	if len(hunks) == 0 {
		return AffectedPaths{}, errors.New("No files were modified.")
	}

	var added, modified, deleted []string

	for _, hunk := range hunks {
		affectedPath := hunk.TargetPath()
		pathAbs := hunk.ResolvePath(cwd).Path()

		switch hunk.Kind {
		case HunkAddFile:
			overwritten, hasOverwritten := readOptionalFileTextForDelta(pathAbs, fsys, &delta.exact)
			if err := writeFileWithMissingParentRetry(fsys, pathAbs, []byte(hunk.Contents)); err != nil {
				// A failed write can leave the target modified, so the delta is
				// no longer exact.
				delta.exact = false
				return AffectedPaths{}, err
			}
			delta.changes = append(delta.changes, AppliedPatchChange{
				Path: pathAbs,
				Change: AppliedPatchFileChange{
					Kind:                  AppliedAdd,
					Content:               hunk.Contents,
					OverwrittenContent:    overwritten,
					HasOverwrittenContent: hasOverwritten,
				},
			})
			added = append(added, affectedPath)

		case HunkDeleteFile:
			noteExistingPathDeltaSupport(pathAbs, fsys, &delta.exact)
			deletedContent, hasDeletedContent := readFileTextOK(pathAbs, fsys)
			if !hasDeletedContent {
				delta.exact = false
			}
			if err := ensureNotDirectory(pathAbs, fsys); err != nil {
				return AffectedPaths{}, fmt.Errorf("Failed to delete file %s: %w", displayPath(pathAbs), err)
			}
			if err := fsys.Remove(pathAbs); err != nil {
				wrapped := fmt.Errorf("Failed to delete file %s: %w", displayPath(pathAbs), err)
				delta.exact = delta.exact && removeFailureWasSideEffectFree(pathAbs, deletedContent, hasDeletedContent, fsys)
				return AffectedPaths{}, wrapped
			}
			if hasDeletedContent {
				delta.changes = append(delta.changes, AppliedPatchChange{
					Path: pathAbs,
					Change: AppliedPatchFileChange{
						Kind:    AppliedDelete,
						Content: deletedContent,
					},
				})
			}
			deleted = append(deleted, affectedPath)

		case HunkUpdateFile:
			noteExistingPathDeltaSupport(pathAbs, fsys, &delta.exact)
			applied, err := deriveNewContentsFromChunks(pathAbs, hunk.Chunks, fsys)
			if err != nil {
				return AffectedPaths{}, err
			}
			if hunk.HasMovePath {
				destAbs := abspath.ResolvePathAgainstBase(hunk.MovePath, cwd.Path()).Path()
				overwrittenMove, hasOverwrittenMove := readOptionalFileTextForDelta(destAbs, fsys, &delta.exact)
				if err := writeFileWithMissingParentRetry(fsys, destAbs, []byte(applied.newContents)); err != nil {
					delta.exact = false
					return AffectedPaths{}, err
				}
				destWriteIdx := len(delta.changes)
				delta.changes = append(delta.changes, AppliedPatchChange{
					Path: destAbs,
					Change: AppliedPatchFileChange{
						Kind:                  AppliedAdd,
						Content:               applied.newContents,
						OverwrittenContent:    overwrittenMove,
						HasOverwrittenContent: hasOverwrittenMove,
					},
				})
				if err := ensureNotDirectory(pathAbs, fsys); err != nil {
					return AffectedPaths{}, fmt.Errorf("Failed to remove original %s: %w", displayPath(pathAbs), err)
				}
				if err := fsys.Remove(pathAbs); err != nil {
					wrapped := fmt.Errorf("Failed to remove original %s: %w", displayPath(pathAbs), err)
					delta.exact = delta.exact && removeFailureWasSideEffectFree(pathAbs, applied.originalContents, true, fsys)
					return AffectedPaths{}, wrapped
				}
				delta.changes[destWriteIdx] = AppliedPatchChange{
					Path: pathAbs,
					Change: AppliedPatchFileChange{
						Kind:                      AppliedUpdate,
						MovePath:                  destAbs,
						HasMovePath:               true,
						OldContent:                applied.originalContents,
						OverwrittenMoveContent:    overwrittenMove,
						HasOverwrittenMoveContent: hasOverwrittenMove,
						NewContent:                applied.newContents,
					},
				}
				modified = append(modified, affectedPath)
			} else {
				if err := fsys.WriteFile(pathAbs, []byte(applied.newContents)); err != nil {
					delta.exact = false
					return AffectedPaths{}, fmt.Errorf("Failed to write file %s: %w", displayPath(pathAbs), err)
				}
				delta.changes = append(delta.changes, AppliedPatchChange{
					Path: pathAbs,
					Change: AppliedPatchFileChange{
						Kind:        AppliedUpdate,
						HasMovePath: false,
						OldContent:  applied.originalContents,
						NewContent:  applied.newContents,
					},
				})
				modified = append(modified, affectedPath)
			}
		}
	}

	return AffectedPaths{Added: added, Modified: modified, Deleted: deleted}, nil
}

// ensureNotDirectory mirrors Rust `ensure_not_directory`.
func ensureNotDirectory(path string, fsys FileSystem) error {
	meta, err := fsys.Metadata(path)
	if err != nil {
		return err
	}
	if meta.IsDirectory {
		return errors.New("path is a directory")
	}
	return nil
}

// removeFailureWasSideEffectFree mirrors Rust
// `remove_failure_was_side_effect_free`: a failed remove is side-effect-free
// only if the target still holds the expected content.
func removeFailureWasSideEffectFree(path string, expected string, hasExpected bool, fsys FileSystem) bool {
	if !hasExpected {
		return false
	}
	content, err := fsys.ReadFileText(path)
	return err == nil && content == expected
}

// readOptionalFileTextForDelta mirrors Rust `read_optional_file_text_for_delta`.
// It returns the file content and true if readable; on a not-found error it
// returns ("", false) without affecting exactness; on any other error it returns
// ("", false) and marks the delta inexact.
func readOptionalFileTextForDelta(path string, fsys FileSystem, exact *bool) (string, bool) {
	noteExistingPathDeltaSupport(path, fsys, exact)
	content, err := fsys.ReadFileText(path)
	switch {
	case err == nil:
		return content, true
	case isNotFound(err):
		return "", false
	default:
		*exact = false
		return "", false
	}
}

// noteExistingPathDeltaSupport mirrors Rust `note_existing_path_delta_support`:
// an existing target that is not a plain regular file (e.g. a directory or
// symlink), or that cannot be stat'd for a non-not-found reason, makes the delta
// inexact.
func noteExistingPathDeltaSupport(path string, fsys FileSystem, exact *bool) {
	meta, err := fsys.Metadata(path)
	switch {
	case err == nil:
		if meta.IsFile && !meta.IsSymlink {
			return
		}
		*exact = false
	case isNotFound(err):
		return
	default:
		*exact = false
	}
}

// writeFileWithMissingParentRetry mirrors Rust
// `write_file_with_missing_parent_retry`: on a not-found write error it creates
// the parent directories and retries once.
func writeFileWithMissingParentRetry(fsys FileSystem, pathAbs string, contents []byte) error {
	err := fsys.WriteFile(pathAbs, contents)
	if err == nil {
		return nil
	}
	if isNotFound(err) {
		if parent, ok := parentDir(pathAbs); ok {
			if mkErr := fsys.CreateDirAll(parent); mkErr != nil {
				return fmt.Errorf("Failed to create parent directories for %s: %w", displayPath(pathAbs), mkErr)
			}
		}
		if werr := fsys.WriteFile(pathAbs, contents); werr != nil {
			return fmt.Errorf("Failed to write file %s: %w", displayPath(pathAbs), werr)
		}
		return nil
	}
	return fmt.Errorf("Failed to write file %s: %w", displayPath(pathAbs), err)
}

// readFileTextOK reads a file, returning ("", false) on any error.
func readFileTextOK(path string, fsys FileSystem) (string, bool) {
	content, err := fsys.ReadFileText(path)
	if err != nil {
		return "", false
	}
	return content, true
}

// wrapIOError wraps an I/O failure as an [*IOError], mirroring Rust's
// conversion of `std::io::Error` into `ApplyPatchError::IoError`.
func wrapIOError(err error) error {
	return &IOError{Context: "I/O error", Err: err}
}

// PrintSummary writes a git-style summary of changes, mirroring Rust
// `print_summary`.
func PrintSummary(affected AffectedPaths, out io.Writer) error {
	if _, err := fmt.Fprintln(out, "Success. Updated the following files:"); err != nil {
		return err
	}
	for _, path := range affected.Added {
		if _, err := fmt.Fprintf(out, "A %s\n", displayPath(path)); err != nil {
			return err
		}
	}
	for _, path := range affected.Modified {
		if _, err := fmt.Fprintf(out, "M %s\n", displayPath(path)); err != nil {
			return err
		}
	}
	for _, path := range affected.Deleted {
		if _, err := fmt.Fprintf(out, "D %s\n", displayPath(path)); err != nil {
			return err
		}
	}
	return nil
}
