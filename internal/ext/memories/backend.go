// Package memories is a faithful, drop-in-compatible Go port of the Codex
// memories extension (Rust crate codex-ext-memories, codex 0.136.0).
//
// It provides the memory skill-tool surface: the list, read, search, and
// add_ad_hoc_note operations backed by the on-disk memories store, the
// developer-instructions prompt fragment (with the embedded read_path.md
// template), and the metric scope classification. JSON response shapes match the
// reference byte-for-byte.
package memories

import "context"

// Tool budget and result constants, mirroring the crate-level constants in
// lib.rs.
const (
	// DefaultListMaxResults is the default cap on list results.
	DefaultListMaxResults = 2_000
	// MaxListResults is the hard cap on list results.
	MaxListResults = 2_000
	// DefaultSearchMaxResults is the default cap on search matches.
	DefaultSearchMaxResults = 200
	// MaxSearchResults is the hard cap on search matches.
	MaxSearchResults = 200
	// DefaultReadMaxTokens is the read token budget (20K tokens).
	DefaultReadMaxTokens = 20_000
	// MemoryToolDeveloperInstructionsSummaryTokenLimit caps the memory summary
	// included in developer instructions.
	MemoryToolDeveloperInstructionsSummaryTokenLimit = 2_500
)

// Tool namespace and operation names, mirroring the crate-level name constants.
const (
	// MemoryToolsNamespace is the tool namespace for memory operations.
	MemoryToolsNamespace = "memories"
	// AddAdHocNoteToolName is the add_ad_hoc_note operation name.
	AddAdHocNoteToolName = "add_ad_hoc_note"
	// ListToolName is the list operation name.
	ListToolName = "list"
	// ReadToolName is the read operation name.
	ReadToolName = "read"
	// SearchToolName is the search operation name.
	SearchToolName = "search"
)

// Backend is the storage interface behind the memories tools. Implementations
// return paths relative to the memory store and enforce their own
// storage-specific access rules. It mirrors the Rust MemoriesBackend trait.
type Backend interface {
	AddAdHocNote(ctx context.Context, req AddAdHocNoteRequest) (AddAdHocNoteResponse, error)
	List(ctx context.Context, req ListRequest) (ListResponse, error)
	Read(ctx context.Context, req ReadRequest) (ReadResponse, error)
	Search(ctx context.Context, req SearchRequest) (SearchResponse, error)
}

// AddAdHocNoteRequest is the input to AddAdHocNote.
type AddAdHocNoteRequest struct {
	Filename string
	Note     string
}

// AddAdHocNoteResponse is the (empty) successful add_ad_hoc_note result. It
// serializes as an empty JSON object, mirroring AddAdHocMemoryNoteResponse.
type AddAdHocNoteResponse struct{}

// ListRequest is the input to List.
type ListRequest struct {
	Path       *string
	Cursor     *string
	MaxResults int
}

// ListResponse is the result of List. Field order and JSON tags mirror
// ListMemoriesResponse.
type ListResponse struct {
	Path       *string       `json:"path"`
	Entries    []MemoryEntry `json:"entries"`
	NextCursor *string       `json:"next_cursor"`
	Truncated  bool          `json:"truncated"`
}

// ReadRequest is the input to Read.
type ReadRequest struct {
	Path       string
	LineOffset int
	MaxLines   *int
	MaxTokens  int
}

// ReadResponse is the result of Read, mirroring ReadMemoryResponse.
type ReadResponse struct {
	Path            string `json:"path"`
	StartLineNumber int    `json:"start_line_number"`
	Content         string `json:"content"`
	Truncated       bool   `json:"truncated"`
}

// SearchRequest is the input to Search.
type SearchRequest struct {
	Queries       []string
	MatchMode     SearchMatchMode
	Path          *string
	Cursor        *string
	ContextLines  int
	CaseSensitive bool
	Normalized    bool
	MaxResults    int
}

// SearchResponse is the result of Search, mirroring SearchMemoriesResponse.
type SearchResponse struct {
	Queries    []string            `json:"queries"`
	MatchMode  SearchMatchMode     `json:"match_mode"`
	Path       *string             `json:"path"`
	Matches    []MemorySearchMatch `json:"matches"`
	NextCursor *string             `json:"next_cursor"`
	Truncated  bool                `json:"truncated"`
}

// MemoryEntry is one directory listing entry, mirroring MemoryEntry.
type MemoryEntry struct {
	Path      string          `json:"path"`
	EntryType MemoryEntryType `json:"entry_type"`
}

// MemoryEntryType discriminates files from directories, mirroring
// MemoryEntryType (serde snake_case).
type MemoryEntryType string

const (
	// EntryFile marks a file entry.
	EntryFile MemoryEntryType = "file"
	// EntryDirectory marks a directory entry.
	EntryDirectory MemoryEntryType = "directory"
)

// MemorySearchMatch is one search match, mirroring MemorySearchMatch.
type MemorySearchMatch struct {
	Path                   string   `json:"path"`
	MatchLineNumber        int      `json:"match_line_number"`
	ContentStartLineNumber int      `json:"content_start_line_number"`
	Content                string   `json:"content"`
	MatchedQueries         []string `json:"matched_queries"`
}
