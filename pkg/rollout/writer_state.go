package rollout

import (
	"context"
	"fmt"
	"os"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// rolloutWriterState is the mutable state owned by the background writer
// goroutine.
//
// Items are first appended to pendingItems; persist/flush/shutdown remove each
// item from that queue only after it is written successfully. I/O failures drop
// the file handle but keep the unwritten suffix so the next barrier can reopen
// the file and retry. This mirrors the Rust `RolloutWriterState`.
type rolloutWriterState struct {
	file         *os.File
	deferredInfo *logFileInfo
	pendingItems []RolloutItem
	meta         *SessionMeta
	cwd          string
	rolloutPath  string
	gitInfo      GitInfoCollector
}

func newRolloutWriterState(file *os.File, deferred *logFileInfo, meta *SessionMeta, cwd, rolloutPath string, gitInfo GitInfoCollector) *rolloutWriterState {
	return &rolloutWriterState{
		file:         file,
		deferredInfo: deferred,
		meta:         meta,
		cwd:          cwd,
		rolloutPath:  rolloutPath,
		gitInfo:      gitInfo,
	}
}

func (s *rolloutWriterState) addItems(items []RolloutItem) {
	s.pendingItems = append(s.pendingItems, items...)
}

// isDeferred reports whether the writer is still in deferred (unmaterialized)
// state.
func (s *rolloutWriterState) isDeferred() bool {
	return s.file == nil && s.deferredInfo != nil
}

func (s *rolloutWriterState) flushIfMaterialized(ctx context.Context) {
	if s.isDeferred() {
		return
	}
	if err := s.flush(ctx); err != nil {
		s.enterRecoveryMode()
	}
}

func (s *rolloutWriterState) persist(ctx context.Context) error {
	return s.writePendingWithRecovery(ctx)
}

func (s *rolloutWriterState) flush(ctx context.Context) error {
	if s.isDeferred() && len(s.pendingItems) == 0 {
		return nil
	}
	return s.writePendingWithRecovery(ctx)
}

func (s *rolloutWriterState) shutdown(ctx context.Context) error {
	if s.isDeferred() && len(s.pendingItems) == 0 {
		return nil
	}
	if err := s.writePendingWithRecovery(ctx); err != nil {
		return err
	}
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}
	return nil
}

// writePendingWithRecovery writes pending items, retrying once after reopening
// the file if the first attempt fails. Mirrors the Rust retry semantics.
func (s *rolloutWriterState) writePendingWithRecovery(ctx context.Context) error {
	if err := s.writePendingOnce(ctx); err != nil {
		s.enterRecoveryMode()
		if retryErr := s.writePendingOnce(ctx); retryErr != nil {
			s.enterRecoveryMode()
			return retryErr
		}
	}
	return nil
}

// enterRecoveryMode drops the open file handle so the next attempt reopens it.
func (s *rolloutWriterState) enterRecoveryMode() {
	if s.file != nil {
		_ = s.file.Close()
	}
	s.file = nil
}

// ensureWriterOpen opens the rollout file if it is not already open.
func (s *rolloutWriterState) ensureWriterOpen() error {
	if s.file != nil {
		return nil
	}
	path := s.rolloutPath
	if s.deferredInfo != nil {
		path = s.deferredInfo.path
	}
	file, err := openLogFile(path)
	if err != nil {
		return err
	}
	s.file = file
	s.deferredInfo = nil
	return nil
}

// writeSessionMetaIfNeeded writes the first session-meta line (collecting git
// info) exactly once.
func (s *rolloutWriterState) writeSessionMetaIfNeeded(ctx context.Context) error {
	if s.meta == nil {
		return nil
	}
	meta := *s.meta
	var git *protocol.GitInfo
	if s.gitInfo != nil {
		git = s.gitInfo(ctx, s.cwd)
	}
	line := SessionMetaLine{Meta: meta, Git: git}
	item := NewSessionMetaItem(line)
	if s.file != nil {
		if err := writeRolloutItem(s.file, item); err != nil {
			return err
		}
	}
	s.meta = nil
	return nil
}

func (s *rolloutWriterState) writePendingOnce(ctx context.Context) error {
	if err := s.ensureWriterOpen(); err != nil {
		return err
	}
	if err := s.writeSessionMetaIfNeeded(ctx); err != nil {
		return err
	}
	if err := s.writePendingItemsOnce(); err != nil {
		return err
	}
	if s.file != nil {
		if err := s.file.Sync(); err != nil {
			return fmt.Errorf("sync rollout file: %w", err)
		}
	}
	return nil
}

func (s *rolloutWriterState) writePendingItemsOnce() error {
	if s.file == nil {
		return errWriterNotOpen
	}
	written := 0
	var writeErr error
	for _, item := range s.pendingItems {
		if err := writeRolloutItem(s.file, item); err != nil {
			writeErr = err
			break
		}
		written++
	}
	if written > 0 {
		s.pendingItems = s.pendingItems[written:]
	}
	return writeErr
}
