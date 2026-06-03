package feedback

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	// DefaultMaxBytes is the ring buffer capacity (4 MiB). Mirrors Rust
	// DEFAULT_MAX_BYTES.
	DefaultMaxBytes = 4 * 1024 * 1024
	// DoctorReportAttachmentFilename is the filename for the redacted
	// `codex doctor --json` attachment. Mirrors Rust
	// DOCTOR_REPORT_ATTACHMENT_FILENAME.
	DoctorReportAttachmentFilename = "codex-doctor-report.json"
	// WindowsSandboxLogAttachmentFilename is the filename for the Windows
	// sandbox log attachment. Mirrors Rust WINDOWS_SANDBOX_LOG_ATTACHMENT_FILENAME.
	WindowsSandboxLogAttachmentFilename = "windows-sandbox.log"
	// FeedbackTagsTarget is the log target that carries structured feedback
	// tags. Mirrors Rust FEEDBACK_TAGS_TARGET.
	FeedbackTagsTarget = "feedback_tags"
	// maxFeedbackTags bounds the number of retained tags. Mirrors Rust
	// MAX_FEEDBACK_TAGS.
	maxFeedbackTags = 64
)

// CodexFeedback owns the log ring buffer and the structured tag map. Mirrors
// Rust `CodexFeedback`. It is safe for concurrent use.
type CodexFeedback struct {
	mu   sync.Mutex
	ring *ringBuffer
	tags map[string]string
}

// NewCodexFeedback constructs a feedback collector with the default 4 MiB
// capacity. Mirrors Rust `CodexFeedback::new`.
func NewCodexFeedback() *CodexFeedback {
	return NewCodexFeedbackWithCapacity(DefaultMaxBytes)
}

// NewCodexFeedbackWithCapacity constructs a feedback collector with an explicit
// capacity. Mirrors Rust `CodexFeedback::with_capacity`.
func NewCodexFeedbackWithCapacity(maxBytes int) *CodexFeedback {
	return &CodexFeedback{
		ring: newRingBuffer(maxBytes),
		tags: map[string]string{},
	}
}

// Write appends log bytes to the ring buffer. It implements io.Writer so it can
// back a logging sink (the Rust FeedbackMakeWriter/FeedbackWriter). It never
// returns a short write or error.
func (f *CodexFeedback) Write(p []byte) (int, error) {
	f.mu.Lock()
	f.ring.pushBytes(p)
	f.mu.Unlock()
	return len(p), nil
}

// RecordTag records a single feedback tag, honoring the maxFeedbackTags cap.
// Mirrors the per-event tag insertion in Rust `FeedbackMetadataLayer::on_event`.
func (f *CodexFeedback) RecordTag(key, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tags) >= maxFeedbackTags {
		if _, ok := f.tags[key]; !ok {
			return
		}
	}
	f.tags[key] = value
}

// RecordTags records multiple feedback tags. Mirrors Rust handling of a batch
// of recorded fields, applying the cap per key in deterministic order.
func (f *CodexFeedback) RecordTags(tags map[string]string) {
	for _, key := range sortedKeys(tags) {
		f.RecordTag(key, tags[key])
	}
}

// Snapshot captures the current logs, tags, and connectivity diagnostics.
// Mirrors Rust `CodexFeedback::snapshot`. When sessionID is nil a synthetic
// "no-active-thread-..." id is generated.
func (f *CodexFeedback) Snapshot(sessionID *string, newThreadID func() string) *FeedbackSnapshot {
	f.mu.Lock()
	bytes := f.ring.snapshotBytes()
	tags := make(map[string]string, len(f.tags))
	for k, v := range f.tags {
		tags[k] = v
	}
	f.mu.Unlock()

	threadID := ""
	if sessionID != nil {
		threadID = *sessionID
	} else if newThreadID != nil {
		threadID = "no-active-thread-" + newThreadID()
	} else {
		threadID = "no-active-thread-"
	}

	return &FeedbackSnapshot{
		bytes:               bytes,
		tags:                tags,
		feedbackDiagnostics: CollectFromEnv(os.LookupEnv),
		ThreadID:            threadID,
	}
}

// FeedbackAttachmentPath references a file to upload, with an optional override
// filename. Mirrors Rust `FeedbackAttachmentPath`.
type FeedbackAttachmentPath struct {
	Path                       string
	AttachmentFilenameOverride *string
}

// FeedbackAttachment is an in-memory attachment. Mirrors Rust
// `FeedbackAttachment`.
type FeedbackAttachment struct {
	Filename    string
	ContentType *string
	Buffer      []byte
}

// FeedbackUploadOptions controls one upload. Mirrors Rust
// `FeedbackUploadOptions`. The caller is responsible for any consent gate before
// setting IncludeLogs or passing diagnostic attachments.
type FeedbackUploadOptions struct {
	Classification       string
	Reason               *string
	Tags                 map[string]string
	IncludeLogs          bool
	ExtraAttachments     []FeedbackAttachment
	ExtraAttachmentPaths []FeedbackAttachmentPath
	SessionSource        *string
	LogsOverride         []byte
}

// FeedbackSnapshot is an immutable capture of logs/tags/diagnostics for upload.
// Mirrors Rust `FeedbackSnapshot`.
type FeedbackSnapshot struct {
	bytes               []byte
	tags                map[string]string
	feedbackDiagnostics FeedbackDiagnostics
	ThreadID            string
}

// AsBytes returns the captured log bytes. Mirrors Rust `as_bytes`.
func (s *FeedbackSnapshot) AsBytes() []byte {
	return s.bytes
}

// FeedbackDiagnostics returns the captured diagnostics. Mirrors Rust accessor.
func (s *FeedbackSnapshot) FeedbackDiagnostics() FeedbackDiagnostics {
	return s.feedbackDiagnostics
}

// WithFeedbackDiagnostics returns a copy of the snapshot with the given
// diagnostics. Mirrors Rust `with_feedback_diagnostics`. The snapshot is
// otherwise immutable, so this returns a new value (the receiver is unchanged).
func (s *FeedbackSnapshot) WithFeedbackDiagnostics(d FeedbackDiagnostics) *FeedbackSnapshot {
	clone := *s
	clone.feedbackDiagnostics = d
	return &clone
}

// FeedbackDiagnosticsAttachmentText renders the diagnostics attachment text when
// logs are included. Mirrors Rust `feedback_diagnostics_attachment_text`.
func (s *FeedbackSnapshot) FeedbackDiagnosticsAttachmentText(includeLogs bool) (string, bool) {
	if !includeLogs {
		return "", false
	}
	return s.feedbackDiagnostics.AttachmentText()
}

// SaveToTempFile writes the captured logs to a temp file and returns its path.
// Mirrors Rust `save_to_temp_file`.
func (s *FeedbackSnapshot) SaveToTempFile() (string, error) {
	dir := os.TempDir()
	filename := fmt.Sprintf("codex-feedback-%s.log", s.ThreadID)
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, s.bytes, 0o600); err != nil {
		return "", fmt.Errorf("feedback: write temp log file: %w", err)
	}
	return path, nil
}
