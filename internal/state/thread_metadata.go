package state

import (
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
)

// SortKey selects which timestamp threads are ordered by when listing.
type SortKey int

const (
	// SortKeyCreatedAt orders by the thread's creation timestamp.
	SortKeyCreatedAt SortKey = iota
	// SortKeyUpdatedAt orders by the thread's last-update timestamp.
	SortKeyUpdatedAt
)

// SortDirection selects ascending or descending ordering when listing threads.
type SortDirection int

const (
	// SortDirectionAsc orders results ascending.
	SortDirectionAsc SortDirection = iota
	// SortDirectionDesc orders results descending.
	SortDirectionDesc
)

// Anchor is a keyset-pagination cursor: the timestamp of the last row returned.
type Anchor struct {
	// TS is the timestamp component of the anchor.
	TS time.Time
}

// ThreadsPage is one page of thread metadata produced by ListThreads.
type ThreadsPage struct {
	// Items are the thread metadata rows in this page.
	Items []ThreadMetadata
	// NextAnchor is the cursor for the next page, or nil when exhausted.
	NextAnchor *Anchor
	// NumScannedRows is the number of rows scanned to produce this page.
	NumScannedRows int
}

// ExtractionOutcome is the result of extracting metadata from a rollout file.
type ExtractionOutcome struct {
	// Metadata is the extracted thread metadata.
	Metadata ThreadMetadata
	// MemoryMode is the explicit memory mode from rollout metadata, if present.
	MemoryMode *string
	// ParseErrors counts rollout lines that failed to parse.
	ParseErrors int
}

// BackfillStats summarizes a backfill operation.
type BackfillStats struct {
	// Scanned is the number of rollout files scanned.
	Scanned int
	// Upserted is the number of rows upserted successfully.
	Upserted int
	// Failed is the number of rows that failed to upsert.
	Failed int
}

// ThreadMetadata is the canonical thread metadata derived from rollout files
// and mirrored into the `threads` table.
//
// Field order matches the Rust `ThreadMetadata` struct for readability; JSON is
// not emitted for this type so only the SQLite column mapping is load-bearing.
type ThreadMetadata struct {
	ID               protocol.ThreadID
	RolloutPath      string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Source           string
	ThreadSource     *rollout.ThreadSource
	AgentNickname    *string
	AgentRole        *string
	AgentPath        *string
	ModelProvider    string
	Model            *string
	ReasoningEffort  *protocol.ReasoningEffort
	Cwd              string
	CliVersion       string
	Title            string
	Preview          *string
	SandboxPolicy    string
	ApprovalMode     string
	TokensUsed       int64
	FirstUserMessage *string
	ArchivedAt       *time.Time
	GitSha           *string
	GitBranch        *string
	GitOriginURL     *string
}

// PreferExistingGitInfo keeps non-nil git fields from existing when reconciling
// rollout-derived metadata, mirroring the Rust method.
func (m *ThreadMetadata) PreferExistingGitInfo(existing *ThreadMetadata) {
	if existing.GitSha != nil {
		m.GitSha = cloneStringPtr(existing.GitSha)
	}
	if existing.GitBranch != nil {
		m.GitBranch = cloneStringPtr(existing.GitBranch)
	}
	if existing.GitOriginURL != nil {
		m.GitOriginURL = cloneStringPtr(existing.GitOriginURL)
	}
}

// DiffFields returns the names of fields that differ between m and other, in
// declaration order, matching the Rust `diff_fields`.
func (m *ThreadMetadata) DiffFields(other *ThreadMetadata) []string {
	var diffs []string
	if m.ID != other.ID {
		diffs = append(diffs, "id")
	}
	if m.RolloutPath != other.RolloutPath {
		diffs = append(diffs, "rollout_path")
	}
	if !m.CreatedAt.Equal(other.CreatedAt) {
		diffs = append(diffs, "created_at")
	}
	if !m.UpdatedAt.Equal(other.UpdatedAt) {
		diffs = append(diffs, "updated_at")
	}
	if m.Source != other.Source {
		diffs = append(diffs, "source")
	}
	if !stringPtrEq(m.AgentNickname, other.AgentNickname) {
		diffs = append(diffs, "agent_nickname")
	}
	if !stringPtrEq(m.AgentRole, other.AgentRole) {
		diffs = append(diffs, "agent_role")
	}
	if !stringPtrEq(m.AgentPath, other.AgentPath) {
		diffs = append(diffs, "agent_path")
	}
	if m.ModelProvider != other.ModelProvider {
		diffs = append(diffs, "model_provider")
	}
	if !stringPtrEq(m.Model, other.Model) {
		diffs = append(diffs, "model")
	}
	if !reasoningEffortPtrEq(m.ReasoningEffort, other.ReasoningEffort) {
		diffs = append(diffs, "reasoning_effort")
	}
	if m.Cwd != other.Cwd {
		diffs = append(diffs, "cwd")
	}
	if m.CliVersion != other.CliVersion {
		diffs = append(diffs, "cli_version")
	}
	if m.Title != other.Title {
		diffs = append(diffs, "title")
	}
	if !stringPtrEq(m.Preview, other.Preview) {
		diffs = append(diffs, "preview")
	}
	if m.SandboxPolicy != other.SandboxPolicy {
		diffs = append(diffs, "sandbox_policy")
	}
	if m.ApprovalMode != other.ApprovalMode {
		diffs = append(diffs, "approval_mode")
	}
	if m.TokensUsed != other.TokensUsed {
		diffs = append(diffs, "tokens_used")
	}
	if !stringPtrEq(m.FirstUserMessage, other.FirstUserMessage) {
		diffs = append(diffs, "first_user_message")
	}
	if !timePtrEq(m.ArchivedAt, other.ArchivedAt) {
		diffs = append(diffs, "archived_at")
	}
	if !stringPtrEq(m.GitSha, other.GitSha) {
		diffs = append(diffs, "git_sha")
	}
	if !stringPtrEq(m.GitBranch, other.GitBranch) {
		diffs = append(diffs, "git_branch")
	}
	if !stringPtrEq(m.GitOriginURL, other.GitOriginURL) {
		diffs = append(diffs, "git_origin_url")
	}
	return diffs
}

// ThreadMetadataBuilder constructs ThreadMetadata from rollout session metadata
// without parsing filenames, mirroring the Rust `ThreadMetadataBuilder`.
type ThreadMetadataBuilder struct {
	ID            protocol.ThreadID
	RolloutPath   string
	CreatedAt     time.Time
	UpdatedAt     *time.Time
	Source        rollout.SessionSource
	ThreadSource  *rollout.ThreadSource
	AgentNickname *string
	AgentRole     *string
	AgentPath     *string
	ModelProvider *string
	Cwd           string
	CliVersion    *string
	SandboxPolicy protocol.SandboxPolicy
	ApprovalMode  protocol.AskForApproval
	ArchivedAt    *time.Time
	GitSha        *string
	GitBranch     *string
	GitOriginURL  *string
}

// NewThreadMetadataBuilder creates a builder with required fields and the same
// defaults as the Rust constructor (read-only sandbox, on-request approval).
func NewThreadMetadataBuilder(
	id protocol.ThreadID,
	rolloutPath string,
	createdAt time.Time,
	source rollout.SessionSource,
) ThreadMetadataBuilder {
	return ThreadMetadataBuilder{
		ID:            id,
		RolloutPath:   rolloutPath,
		CreatedAt:     createdAt,
		Source:        source,
		Cwd:           "",
		SandboxPolicy: protocol.NewReadOnlySandboxPolicy(false),
		ApprovalMode:  protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest},
	}
}

// Build produces canonical ThreadMetadata, filling missing values from
// defaults. defaultProvider is used when the builder has no model provider.
func (b *ThreadMetadataBuilder) Build(defaultProvider string) ThreadMetadata {
	source := enumToString(b.Source)
	sandboxPolicy := enumToString(b.SandboxPolicy)
	approvalMode := enumToString(b.ApprovalMode)
	createdAt := canonicalizeDatetime(b.CreatedAt)
	updatedAt := createdAt
	if b.UpdatedAt != nil {
		updatedAt = canonicalizeDatetime(*b.UpdatedAt)
	}

	agentPath := cloneStringPtr(b.AgentPath)
	if agentPath == nil {
		if p := b.Source.AgentPath(); p != nil {
			s := p.String()
			agentPath = &s
		}
	}

	modelProvider := defaultProvider
	if b.ModelProvider != nil {
		modelProvider = *b.ModelProvider
	}

	cliVersion := ""
	if b.CliVersion != nil {
		cliVersion = *b.CliVersion
	}

	var archivedAt *time.Time
	if b.ArchivedAt != nil {
		c := canonicalizeDatetime(*b.ArchivedAt)
		archivedAt = &c
	}

	return ThreadMetadata{
		ID:               b.ID,
		RolloutPath:      b.RolloutPath,
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
		Source:           source,
		ThreadSource:     cloneThreadSourcePtr(b.ThreadSource),
		AgentNickname:    cloneStringPtr(b.AgentNickname),
		AgentRole:        cloneStringPtr(b.AgentRole),
		AgentPath:        agentPath,
		ModelProvider:    modelProvider,
		Model:            nil,
		ReasoningEffort:  nil,
		Cwd:              b.Cwd,
		CliVersion:       cliVersion,
		Title:            "",
		Preview:          nil,
		SandboxPolicy:    sandboxPolicy,
		ApprovalMode:     approvalMode,
		TokensUsed:       0,
		FirstUserMessage: nil,
		ArchivedAt:       archivedAt,
		GitSha:           cloneStringPtr(b.GitSha),
		GitBranch:        cloneStringPtr(b.GitBranch),
		GitOriginURL:     cloneStringPtr(b.GitOriginURL),
	}
}

// anchorFromItem returns the pagination cursor for item given sortKey.
func anchorFromItem(item *ThreadMetadata, sortKey SortKey) *Anchor {
	ts := item.CreatedAt
	if sortKey == SortKeyUpdatedAt {
		ts = item.UpdatedAt
	}
	return &Anchor{TS: ts}
}

func cloneStringPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneThreadSourcePtr(p *rollout.ThreadSource) *rollout.ThreadSource {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func stringPtrEq(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func timePtrEq(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func reasoningEffortPtrEq(a, b *protocol.ReasoningEffort) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
