package applypatch

// FileChangeKind identifies which kind of change an [ApplyPatchFileChange]
// describes, mirroring the variants of Rust `ApplyPatchFileChange`.
type FileChangeKind int

const (
	// FileChangeAdd mirrors `ApplyPatchFileChange::Add`.
	FileChangeAdd FileChangeKind = iota
	// FileChangeDelete mirrors `ApplyPatchFileChange::Delete`.
	FileChangeDelete
	// FileChangeUpdate mirrors `ApplyPatchFileChange::Update`.
	FileChangeUpdate
)

// ApplyPatchFileChange is the proposed change for a single file, mirroring the
// Rust `ApplyPatchFileChange` enum. The active fields depend on Kind:
//
//   - FileChangeAdd: Content
//   - FileChangeDelete: Content
//   - FileChangeUpdate: UnifiedDiff, MovePath (optional), NewContent
type ApplyPatchFileChange struct {
	Kind FileChangeKind

	// Content is the full file content for Add and Delete changes.
	Content string

	// UnifiedDiff is the diff describing an Update change.
	UnifiedDiff string
	// MovePath / HasMovePath describe the optional rename destination for an
	// Update change (Rust Option<PathBuf>).
	MovePath    string
	HasMovePath bool
	// NewContent is the resulting content after an Update change is applied.
	NewContent string
}

// AppliedPatchDelta records the textual file changes that were actually
// committed while applying a patch, mirroring Rust `AppliedPatchDelta`. The
// exact flag reports whether the recorded delta is a complete and accurate
// description of every committed mutation; a failed write or an unreadable
// target makes the aggregate inexact.
//
// AppliedPatchDelta values are produced by the apply functions and should be
// treated as read-only by callers (use the accessor methods).
type AppliedPatchDelta struct {
	changes []AppliedPatchChange
	exact   bool
}

// newAppliedPatchDelta mirrors Rust `AppliedPatchDelta::new`.
func newAppliedPatchDelta(changes []AppliedPatchChange, exact bool) AppliedPatchDelta {
	return AppliedPatchDelta{changes: changes, exact: exact}
}

// emptyAppliedPatchDelta mirrors Rust `AppliedPatchDelta::empty`.
func emptyAppliedPatchDelta() AppliedPatchDelta {
	return AppliedPatchDelta{changes: nil, exact: true}
}

// Changes returns the committed changes in application order. The returned slice
// is the internal backing slice; callers must not mutate it.
func (d AppliedPatchDelta) Changes() []AppliedPatchChange {
	return d.changes
}

// IsEmpty reports whether no changes were committed.
func (d AppliedPatchDelta) IsEmpty() bool {
	return len(d.changes) == 0
}

// IsExact reports whether the recorded delta exactly describes every committed
// mutation, mirroring Rust `AppliedPatchDelta::is_exact`.
func (d AppliedPatchDelta) IsExact() bool {
	return d.exact
}

// append mirrors Rust `AppliedPatchDelta::append`: it concatenates the changes
// and conjoins the exactness. It returns a new value rather than mutating the
// receiver, per the immutability convention.
func (d AppliedPatchDelta) append(other AppliedPatchDelta) AppliedPatchDelta {
	merged := make([]AppliedPatchChange, 0, len(d.changes)+len(other.changes))
	merged = append(merged, d.changes...)
	merged = append(merged, other.changes...)
	return AppliedPatchDelta{changes: merged, exact: d.exact && other.exact}
}

// AppliedPatchChange is a single committed file change, preserved in the order
// it was applied, mirroring Rust `AppliedPatchChange`.
type AppliedPatchChange struct {
	Path   string
	Change AppliedPatchFileChange
}

// AppliedFileChangeKind identifies the variant of [AppliedPatchFileChange],
// mirroring Rust `AppliedPatchFileChange`.
type AppliedFileChangeKind int

const (
	// AppliedAdd mirrors `AppliedPatchFileChange::Add`.
	AppliedAdd AppliedFileChangeKind = iota
	// AppliedDelete mirrors `AppliedPatchFileChange::Delete`.
	AppliedDelete
	// AppliedUpdate mirrors `AppliedPatchFileChange::Update`.
	AppliedUpdate
)

// AppliedPatchFileChange describes a committed change, mirroring the Rust
// `AppliedPatchFileChange` enum. Active fields depend on Kind:
//
//   - AppliedAdd: Content, OverwrittenContent (optional)
//   - AppliedDelete: Content
//   - AppliedUpdate: MovePath (optional), OldContent,
//     OverwrittenMoveContent (optional), NewContent
type AppliedPatchFileChange struct {
	Kind AppliedFileChangeKind

	// Content is the written content (Add) or the removed content (Delete).
	Content string
	// OverwrittenContent / HasOverwrittenContent record content that an Add
	// overwrote, if any (Rust Option<String>).
	OverwrittenContent    string
	HasOverwrittenContent bool

	// Update fields.
	MovePath    string
	HasMovePath bool
	OldContent  string
	// OverwrittenMoveContent / HasOverwrittenMoveContent record content that a
	// move destination overwrote, if any.
	OverwrittenMoveContent    string
	HasOverwrittenMoveContent bool
	NewContent                string
}

// AffectedPaths records which files were added, modified, or deleted by a patch,
// preserving the path spelling from the patch for user-facing summaries. It
// mirrors Rust `AffectedPaths`.
type AffectedPaths struct {
	Added    []string
	Modified []string
	Deleted  []string
}

// ApplyFailure is a failed patch application together with the textual
// mutations that were definitely committed before the failure was observed. It
// mirrors Rust `ApplyPatchFailure` and implements error.
type ApplyFailure struct {
	Err   error
	Delta AppliedPatchDelta
}

// Error implements error, rendering the underlying error like Rust's
// `#[error("{error}")]`.
func (f *ApplyFailure) Error() string {
	return f.Err.Error()
}

// Unwrap exposes the underlying error for errors.Is/As.
func (f *ApplyFailure) Unwrap() error {
	return f.Err
}

// newApplyFailure mirrors Rust `ApplyPatchFailure::new`.
func newApplyFailure(err error, delta AppliedPatchDelta) *ApplyFailure {
	return &ApplyFailure{Err: err, Delta: delta}
}

// applyFailureWithoutDelta mirrors Rust `ApplyPatchFailure::without_delta`.
func applyFailureWithoutDelta(err error) *ApplyFailure {
	return &ApplyFailure{Err: err, Delta: emptyAppliedPatchDelta()}
}
