package state

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
)

// insertThreadColumns is the shared INSERT prefix for thread upserts. It writes
// both the legacy second-precision and the millisecond timestamp columns, and a
// memory_mode value, matching the Rust upsert statements.
const insertThreadColumns = `
INSERT INTO threads (
    id,
    rollout_path,
    created_at,
    updated_at,
    created_at_ms,
    updated_at_ms,
    source,
    thread_source,
    agent_nickname,
    agent_role,
    agent_path,
    model_provider,
    model,
    reasoning_effort,
    cwd,
    cli_version,
    title,
    preview,
    sandbox_policy,
    approval_mode,
    tokens_used,
    first_user_message,
    archived,
    archived_at,
    git_sha,
    git_branch,
    git_origin_url,
    memory_mode
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// threadInsertArgs builds the bind arguments for the thread upsert statements in
// the same column order as insertThreadColumns. updatedAt is the allocated
// updated_at value; memoryMode is the creation-time memory mode.
func threadInsertArgs(metadata *ThreadMetadata, updatedAt time.Time, memoryMode string) []any {
	preview := metadataPreview(metadata)
	return []any{
		metadata.ID.String(),
		metadata.RolloutPath,
		datetimeToEpochSeconds(metadata.CreatedAt),
		datetimeToEpochSeconds(updatedAt),
		datetimeToEpochMillis(metadata.CreatedAt),
		datetimeToEpochMillis(updatedAt),
		metadata.Source,
		threadSourceArg(metadata.ThreadSource),
		nullableString(metadata.AgentNickname),
		nullableString(metadata.AgentRole),
		nullableString(metadata.AgentPath),
		metadata.ModelProvider,
		nullableString(metadata.Model),
		reasoningEffortArg(metadata.ReasoningEffort),
		metadata.Cwd,
		metadata.CliVersion,
		metadata.Title,
		preview,
		metadata.SandboxPolicy,
		metadata.ApprovalMode,
		metadata.TokensUsed,
		firstUserMessageArg(metadata.FirstUserMessage),
		boolToInt(metadata.ArchivedAt != nil),
		archivedAtArg(metadata.ArchivedAt),
		nullableString(metadata.GitSha),
		nullableString(metadata.GitBranch),
		nullableString(metadata.GitOriginURL),
		memoryMode,
	}
}

// metadataPreview chooses the preview to persist: the explicit preview, else the
// first user message, else empty, mirroring the Rust `metadata_preview`.
func metadataPreview(metadata *ThreadMetadata) string {
	if metadata.Preview != nil {
		return *metadata.Preview
	}
	if metadata.FirstUserMessage != nil {
		return *metadata.FirstUserMessage
	}
	return ""
}

// scanThreadMetadata maps a thread row (in threadSelectColumns order) into a
// ThreadMetadata, mirroring the Rust `ThreadRow` + `TryFrom`. The created_at and
// updated_at columns hold millisecond timestamps (aliased from *_ms columns),
// while archived_at holds a second-precision timestamp.
func scanThreadMetadata(scan func(dest ...any) error) (*ThreadMetadata, error) {
	var (
		id               string
		rolloutPath      string
		createdAt        int64
		updatedAt        int64
		source           string
		threadSource     sql.NullString
		agentNickname    sql.NullString
		agentRole        sql.NullString
		agentPath        sql.NullString
		modelProvider    string
		model            sql.NullString
		reasoningEffort  sql.NullString
		cwd              string
		cliVersion       string
		title            string
		preview          string
		sandboxPolicy    string
		approvalMode     string
		tokensUsed       int64
		firstUserMessage string
		archivedAt       sql.NullInt64
		gitSha           sql.NullString
		gitBranch        sql.NullString
		gitOriginURL     sql.NullString
	)
	if err := scan(
		&id, &rolloutPath, &createdAt, &updatedAt, &source, &threadSource,
		&agentNickname, &agentRole, &agentPath, &modelProvider, &model,
		&reasoningEffort, &cwd, &cliVersion, &title, &preview, &sandboxPolicy,
		&approvalMode, &tokensUsed, &firstUserMessage, &archivedAt, &gitSha,
		&gitBranch, &gitOriginURL,
	); err != nil {
		return nil, err
	}

	createdTime, err := epochMillisToDatetime(createdAt)
	if err != nil {
		return nil, fmt.Errorf("decode created_at: %w", err)
	}
	updatedTime, err := epochMillisToDatetime(updatedAt)
	if err != nil {
		return nil, fmt.Errorf("decode updated_at: %w", err)
	}

	var ts *rollout.ThreadSource
	if threadSource.Valid {
		parsed, perr := rollout.ParseThreadSource(threadSource.String)
		if perr != nil {
			return nil, fmt.Errorf("parse thread_source: %w", perr)
		}
		ts = &parsed
	}

	var archivedTime *time.Time
	if archivedAt.Valid {
		at, aerr := epochSecondsToDatetime(archivedAt.Int64)
		if aerr != nil {
			return nil, fmt.Errorf("decode archived_at: %w", aerr)
		}
		archivedTime = &at
	}

	meta := &ThreadMetadata{
		ID:               protocol.NewThreadID(id),
		RolloutPath:      rolloutPath,
		CreatedAt:        createdTime,
		UpdatedAt:        updatedTime,
		Source:           source,
		ThreadSource:     ts,
		AgentNickname:    nullStringToPtr(agentNickname),
		AgentRole:        nullStringToPtr(agentRole),
		AgentPath:        nullStringToPtr(agentPath),
		ModelProvider:    modelProvider,
		Model:            nullStringToPtr(model),
		ReasoningEffort:  parseStoredReasoningEffort(reasoningEffort),
		Cwd:              cwd,
		CliVersion:       cliVersion,
		Title:            title,
		Preview:          emptyToNil(preview),
		SandboxPolicy:    sandboxPolicy,
		ApprovalMode:     approvalMode,
		TokensUsed:       tokensUsed,
		FirstUserMessage: emptyToNil(firstUserMessage),
		ArchivedAt:       archivedTime,
		GitSha:           nullStringToPtr(gitSha),
		GitBranch:        nullStringToPtr(gitBranch),
		GitOriginURL:     nullStringToPtr(gitOriginURL),
	}
	return meta, nil
}

func threadSourceArg(ts *rollout.ThreadSource) any {
	if ts == nil {
		return nil
	}
	return ts.String()
}

func reasoningEffortArg(effort *protocol.ReasoningEffort) any {
	if effort == nil {
		return nil
	}
	return enumToString(*effort)
}

func archivedAtArg(t *time.Time) any {
	if t == nil {
		return nil
	}
	return datetimeToEpochSeconds(*t)
}

func firstUserMessageArg(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func nullableString(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullStringToPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

func emptyToNil(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// parseStoredReasoningEffort parses a reasoning_effort column, ignoring unknown
// values (matching the Rust `.parse().ok()` behavior).
func parseStoredReasoningEffort(ns sql.NullString) *protocol.ReasoningEffort {
	if !ns.Valid {
		return nil
	}
	switch protocol.ReasoningEffort(ns.String) {
	case protocol.ReasoningEffortNone,
		protocol.ReasoningEffortMinimal,
		protocol.ReasoningEffortLow,
		protocol.ReasoningEffortMedium,
		protocol.ReasoningEffortHigh,
		protocol.ReasoningEffortXHigh:
		effort := protocol.ReasoningEffort(ns.String)
		return &effort
	default:
		return nil
	}
}
