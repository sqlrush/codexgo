package rollout

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// rolloutTimestampLayout is the Go time layout for rollout line timestamps:
// `YYYY-MM-DDThh:mm:ss.SSSZ` in UTC with millisecond precision.
const rolloutTimestampLayout = "2006-01-02T15:04:05.000Z"

// rolloutFilenameLayout is the Go time layout for the filename timestamp:
// `YYYY-MM-DDThh-mm-ss` (local time, dashes instead of colons for filesystem
// compatibility).
const rolloutFilenameLayout = "2006-01-02T15-04-05"

// nowUTC and nowLocal are overridable clocks so tests can pin time. They are
// only assigned in tests.
var (
	nowUTC   = func() time.Time { return time.Now().UTC() }
	nowLocal = func() time.Time { return time.Now() }
)

// CreateParams describes a brand-new session to record.
//
// Mirrors the fields of the Rust `RolloutRecorderParams::Create` variant. The
// `Originator` and `CliVersion` are injected by the caller (the Rust crate reads
// them from the login client and the build environment, respectively).
type CreateParams struct {
	ConversationID   protocol.ThreadID
	ForkedFromID     *protocol.ThreadID
	Source           SessionSource
	ThreadSource     *ThreadSource
	BaseInstructions BaseInstructions
	DynamicTools     []protocol.DynamicToolSpec
	Originator       string
	CliVersion       string
	// GitInfo collects git metadata for the session-meta line; nil records none.
	GitInfo GitInfoCollector
}

// RolloutRecorder writes canonical session rollout items to a JSONL file using a
// background writer goroutine.
//
// Mirrors the Rust `RolloutRecorder`. For newly created sessions the file is not
// opened until the first recordable item is flushed/persisted (deferred
// materialization). For resumed sessions the existing file is opened
// immediately.
//
// The recorder is safe to share; commands are serialized through a channel and
// applied by a single writer goroutine.
type RolloutRecorder struct {
	commands    chan rolloutCmd
	rolloutPath string
}

// rolloutCmd is a command processed by the background writer goroutine.
type rolloutCmd struct {
	kind  rolloutCmdKind
	items []RolloutItem
	ack   chan error
}

type rolloutCmdKind int

const (
	cmdAddItems rolloutCmdKind = iota
	cmdPersist
	cmdFlush
	cmdShutdown
)

// NewRecorderForCreate creates a recorder for a new session. The rollout file is
// materialized lazily on the first flush/persist that has pending items.
func NewRecorderForCreate(ctx context.Context, config RolloutConfigView, params CreateParams) (*RolloutRecorder, error) {
	info, err := precomputeLogFileInfo(config, params.ConversationID)
	if err != nil {
		return nil, err
	}

	timestamp := info.timestamp.UTC().Format(rolloutTimestampLayout)

	mp := config.ModelProviderID()
	modelProvider := &mp

	baseInstructions := params.BaseInstructions
	var dynamicTools *[]protocol.DynamicToolSpec
	if len(params.DynamicTools) > 0 {
		tools := append([]protocol.DynamicToolSpec(nil), params.DynamicTools...)
		dynamicTools = &tools
	}
	var memoryMode *string
	if !config.GenerateMemories() {
		disabled := "disabled"
		memoryMode = &disabled
	}

	meta := SessionMeta{
		ID:               info.conversationID,
		ForkedFromID:     params.ForkedFromID,
		Timestamp:        timestamp,
		Cwd:              config.Cwd(),
		Originator:       params.Originator,
		CliVersion:       params.CliVersion,
		Source:           params.Source,
		ThreadSource:     params.ThreadSource,
		AgentNickname:    params.Source.Nickname(),
		AgentRole:        params.Source.AgentRole(),
		AgentPath:        agentPathString(params.Source.AgentPath()),
		ModelProvider:    modelProvider,
		BaseInstructions: &baseInstructions,
		DynamicTools:     dynamicTools,
		MemoryMode:       memoryMode,
	}

	return spawnRecorder(ctx, nil, &info, &meta, config.Cwd(), info.path, params.GitInfo), nil
}

// NewRecorderForResume opens an existing rollout file for appending.
func NewRecorderForResume(ctx context.Context, path string) (*RolloutRecorder, error) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open rollout file for resume: %w", err)
	}
	return spawnRecorder(ctx, file, nil, nil, "", path, nil), nil
}

func agentPathString(p *protocol.AgentPath) *string {
	if p == nil {
		return nil
	}
	s := p.String()
	return &s
}

func spawnRecorder(ctx context.Context, file *os.File, deferred *logFileInfo, meta *SessionMeta, cwd, rolloutPath string, gitInfo GitInfoCollector) *RolloutRecorder {
	state := newRolloutWriterState(file, deferred, meta, cwd, rolloutPath, gitInfo)
	commands := make(chan rolloutCmd, 256)

	go runRolloutWriter(ctx, commands, state)

	return &RolloutRecorder{
		commands:    commands,
		rolloutPath: rolloutPath,
	}
}

// RolloutPath returns the path of the rollout file.
func (r *RolloutRecorder) RolloutPath() string { return r.rolloutPath }

// RecordCanonicalItems queues canonical rollout items to be appended. Items are
// written asynchronously; use Flush to ensure they are committed.
func (r *RolloutRecorder) RecordCanonicalItems(ctx context.Context, items []RolloutItem) error {
	if len(items) == 0 {
		return nil
	}
	cmd := rolloutCmd{kind: cmdAddItems, items: append([]RolloutItem(nil), items...)}
	return r.sendNoAck(ctx, cmd)
}

// Persist materializes the rollout file and writes all buffered items. It is
// idempotent: if materialization fails, items stay buffered for a later retry.
func (r *RolloutRecorder) Persist(ctx context.Context) error {
	return r.sendWithAck(ctx, cmdPersist)
}

// Flush writes all queued items and waits until they are committed.
func (r *RolloutRecorder) Flush(ctx context.Context) error {
	return r.sendWithAck(ctx, cmdFlush)
}

// Shutdown drains pending items and stops the writer goroutine.
func (r *RolloutRecorder) Shutdown(ctx context.Context) error {
	return r.sendWithAck(ctx, cmdShutdown)
}

func (r *RolloutRecorder) sendNoAck(ctx context.Context, cmd rolloutCmd) error {
	select {
	case r.commands <- cmd:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("failed to queue rollout items: %w", ctx.Err())
	}
}

func (r *RolloutRecorder) sendWithAck(ctx context.Context, kind rolloutCmdKind) error {
	ack := make(chan error, 1)
	cmd := rolloutCmd{kind: kind, ack: ack}
	select {
	case r.commands <- cmd:
	case <-ctx.Done():
		return fmt.Errorf("failed to queue rollout command: %w", ctx.Err())
	}
	select {
	case err := <-ack:
		return err
	case <-ctx.Done():
		return fmt.Errorf("failed waiting for rollout command: %w", ctx.Err())
	}
}

// runRolloutWriter is the background writer loop. It owns the writer state and
// processes commands until shutdown or the channel closes.
func runRolloutWriter(ctx context.Context, commands chan rolloutCmd, state *rolloutWriterState) {
	for {
		select {
		case <-ctx.Done():
			return
		case cmd, ok := <-commands:
			if !ok {
				return
			}
			switch cmd.kind {
			case cmdAddItems:
				state.addItems(cmd.items)
				state.flushIfMaterialized(ctx)
			case cmdPersist:
				cmd.ack <- state.persist(ctx)
			case cmdFlush:
				cmd.ack <- state.flush(ctx)
			case cmdShutdown:
				err := state.shutdown(ctx)
				cmd.ack <- err
				if err == nil {
					return
				}
			}
		}
	}
}

// logFileInfo precomputes the rollout file path and metadata for a new session.
type logFileInfo struct {
	path           string
	conversationID protocol.ThreadID
	timestamp      time.Time
}

// precomputeLogFileInfo resolves the rollout file path
// (~/.codex/sessions/YYYY/MM/DD/rollout-...jsonl) and returns the start
// timestamp.
func precomputeLogFileInfo(config RolloutConfigView, conversationID protocol.ThreadID) (logFileInfo, error) {
	timestamp := nowLocal()
	dir := filepath.Join(
		config.CodexHome(),
		SessionsSubdir,
		fmt.Sprintf("%04d", timestamp.Year()),
		fmt.Sprintf("%02d", int(timestamp.Month())),
		fmt.Sprintf("%02d", timestamp.Day()),
	)
	dateStr := timestamp.Format(rolloutFilenameLayout)
	filename := fmt.Sprintf("rollout-%s-%s.jsonl", dateStr, conversationID.String())
	return logFileInfo{
		path:           filepath.Join(dir, filename),
		conversationID: conversationID,
		timestamp:      timestamp,
	}, nil
}

// openLogFile creates the parent directory (if needed) and opens the rollout
// file for appending.
func openLogFile(path string) (*os.File, error) {
	parent := filepath.Dir(path)
	if parent == "" {
		return nil, fmt.Errorf("rollout path has no parent: %s", path)
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("create rollout parent dir: %w", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open rollout file: %w", err)
	}
	return file, nil
}

// GitInfoCollector gathers git info for cwd when cwd is inside a repository and
// returns nil otherwise. It is supplied by the caller through
// [CreateParams.GitInfo]; the rollout package deliberately does not import the
// git backend so that consumers which only need the wire types (session meta,
// rollout items, policy) stay free of that dependency (spec 50 D0.9 seam S3).
// The Rust `write_session_meta` calls `collect_git_info` directly; the Go port
// keeps that behaviour by having the local thread store pass
// `gitutils.CollectGitInfo` here. A nil collector records no git info.
type GitInfoCollector func(ctx context.Context, cwd string) *protocol.GitInfo

// errWriterNotOpen is returned when a write is attempted with no open file.
var errWriterNotOpen = errors.New("rollout writer is not open")
