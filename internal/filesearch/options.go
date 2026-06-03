package filesearch

// Default option values, mirroring the Rust FileSearchOptions::default.
const (
	// DefaultLimit is the default maximum number of results returned.
	DefaultLimit = 20
	// DefaultThreads is the default worker thread count. Empirically the I/O of
	// the tree walk is the bottleneck, so a small value is used.
	DefaultThreads = 2
)

// FileSearchOptions configures a search.
//
// Limit and Threads must be positive; non-positive values are normalized to the
// defaults by Run so callers cannot accidentally request zero results or zero
// workers (the Rust crate enforces this with NonZero types).
type FileSearchOptions struct {
	// Limit is the maximum number of matches to return.
	Limit int
	// Exclude holds gitignore-style patterns. Any path matching one of these is
	// dropped from the results, regardless of gitignore handling. This mirrors
	// the Rust crate's OverrideBuilder negation behavior.
	Exclude []string
	// Threads is the number of concurrent walker workers.
	Threads int
	// ComputeIndices toggles computation of matched-character indices for
	// highlighting. When false, FileMatch.Indices is nil.
	ComputeIndices bool
	// RespectGitignore toggles .gitignore processing. When true, .gitignore
	// files are honored only when the traversed tree is inside a git repository
	// (a .git directory exists at or above a root), matching git's own
	// semantics. When false, all gitignore processing is disabled.
	RespectGitignore bool
}

// DefaultOptions returns a FileSearchOptions populated with the same defaults as
// the Rust FileSearchOptions::default.
func DefaultOptions() FileSearchOptions {
	return FileSearchOptions{
		Limit:            DefaultLimit,
		Exclude:          nil,
		Threads:          DefaultThreads,
		ComputeIndices:   false,
		RespectGitignore: true,
	}
}

// normalized returns a copy of o with non-positive Limit/Threads replaced by
// their defaults and Exclude copied defensively. The receiver is not mutated.
func (o FileSearchOptions) normalized() FileSearchOptions {
	out := o
	if out.Limit <= 0 {
		out.Limit = DefaultLimit
	}
	if out.Threads <= 0 {
		out.Threads = DefaultThreads
	}
	if len(o.Exclude) > 0 {
		out.Exclude = append([]string(nil), o.Exclude...)
	} else {
		out.Exclude = nil
	}
	return out
}
