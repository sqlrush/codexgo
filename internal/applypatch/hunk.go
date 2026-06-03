package applypatch

import (
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// HunkKind identifies which kind of change a [Hunk] describes, mirroring the
// variants of the Rust `Hunk` enum.
type HunkKind int

const (
	// HunkAddFile mirrors `Hunk::AddFile`.
	HunkAddFile HunkKind = iota
	// HunkDeleteFile mirrors `Hunk::DeleteFile`.
	HunkDeleteFile
	// HunkUpdateFile mirrors `Hunk::UpdateFile`.
	HunkUpdateFile
)

// Hunk is a single parsed change from a patch envelope. It corresponds to the
// Rust `Hunk` enum; the active fields depend on [Hunk.Kind]:
//
//   - HunkAddFile: Path, Contents
//   - HunkDeleteFile: Path
//   - HunkUpdateFile: Path, MovePath (optional), Chunks
//
// Path values preserve the spelling from the patch (relative or absolute) and
// are not normalized until resolved against a working directory via
// [Hunk.ResolvePath].
type Hunk struct {
	Kind HunkKind

	// Path is the target file path as written in the patch.
	Path string

	// Contents is the full new file contents for HunkAddFile hunks (each added
	// line followed by '\n').
	Contents string

	// MovePath, when non-empty, is the rename destination for HunkUpdateFile
	// hunks. HasMovePath reports whether a move destination was specified (to
	// distinguish "" from absent, matching Rust's Option<PathBuf>).
	MovePath    string
	HasMovePath bool

	// Chunks holds the change chunks for HunkUpdateFile hunks, in file order.
	Chunks []UpdateFileChunk
}

// UpdateFileChunk is a contiguous change block within an update hunk, mirroring
// the Rust `UpdateFileChunk` struct.
type UpdateFileChunk struct {
	// ChangeContext is an optional single line of context (typically a class,
	// method, or function definition) used to narrow down the chunk position.
	// HasChangeContext distinguishes an empty context ("@@ ") from no context
	// ("@@" or an implicit first chunk), matching Rust's Option<String>.
	ChangeContext    string
	HasChangeContext bool

	// OldLines is the contiguous block of lines to replace; it must occur
	// strictly after ChangeContext.
	OldLines []string
	// NewLines is the replacement block.
	NewLines []string

	// IsEndOfFile, when true, requires OldLines to occur at the end of the
	// source file (with tolerance around trailing newlines).
	IsEndOfFile bool
}

// TargetPath returns the path affected by this hunk, using the move destination
// for rename hunks. It mirrors Rust `Hunk::path`.
func (h Hunk) TargetPath() string {
	if h.Kind == HunkUpdateFile && h.HasMovePath {
		return h.MovePath
	}
	return h.Path
}

// ResolvePath resolves the hunk's path against cwd, mirroring Rust
// `Hunk::resolve_path`. For update hunks it resolves the source Path (not the
// move destination); for add/delete hunks it resolves TargetPath (which equals
// Path).
func (h Hunk) ResolvePath(cwd abspath.AbsolutePathBuf) abspath.AbsolutePathBuf {
	var path string
	switch h.Kind {
	case HunkUpdateFile:
		path = h.Path
	default:
		path = h.TargetPath()
	}
	return abspath.ResolvePathAgainstBase(path, cwd.Path())
}

// displayPath renders a path the way Rust's `Path::display` would for messages.
// On Unix this is the path verbatim; filepath.Clean is intentionally NOT applied
// so the path is shown exactly as written in the patch.
func displayPath(p string) string {
	return p
}
