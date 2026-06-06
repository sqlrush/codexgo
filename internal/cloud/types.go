// Package cloud is a faithful Go port of the Rust `codex-cloud-tasks-client`
// (plus the mock client). It exposes the cloud-tasks data model, the
// CloudBackend interface, an HTTP-backed implementation, and a mock backend
// selectable via CODEXGO_CLOUD_TASKS_MODE=mock.
//
// JSON, on-disk, wire formats, and env vars match the reference codex exactly.
package cloud

import (
	"encoding/json"
	"fmt"
	"time"
)

// TaskID is a transparent wrapper around a task identifier string. It mirrors
// the Rust `TaskId` (serde transparent).
type TaskID string

// TaskStatus is the high-level state of a task. It mirrors the Rust
// `TaskStatus` (serde kebab-case).
type TaskStatus string

const (
	// TaskStatusPending indicates the task has not produced a result yet.
	TaskStatusPending TaskStatus = "pending"
	// TaskStatusReady indicates the task completed and has a result.
	TaskStatusReady TaskStatus = "ready"
	// TaskStatusApplied indicates the task's diff was applied.
	TaskStatusApplied TaskStatus = "applied"
	// TaskStatusError indicates the task failed.
	TaskStatusError TaskStatus = "error"
)

// TaskSummary describes a task in a list or detail view. It mirrors the Rust
// `TaskSummary`.
type TaskSummary struct {
	ID               TaskID      `json:"id"`
	Title            string      `json:"title"`
	Status           TaskStatus  `json:"status"`
	UpdatedAt        time.Time   `json:"updated_at"`
	EnvironmentID    *string     `json:"environment_id"`
	EnvironmentLabel *string     `json:"environment_label"`
	Summary          DiffSummary `json:"summary"`
	IsReview         bool        `json:"is_review"`
	AttemptTotal     *int        `json:"attempt_total"`
}

// AttemptStatus is the status of a single assistant attempt. It mirrors the Rust
// `AttemptStatus`.
type AttemptStatus int

const (
	// AttemptStatusPending indicates the attempt has not started.
	AttemptStatusPending AttemptStatus = iota
	// AttemptStatusInProgress indicates the attempt is running.
	AttemptStatusInProgress
	// AttemptStatusCompleted indicates the attempt finished successfully.
	AttemptStatusCompleted
	// AttemptStatusFailed indicates the attempt failed.
	AttemptStatusFailed
	// AttemptStatusCancelled indicates the attempt was cancelled.
	AttemptStatusCancelled
	// AttemptStatusUnknown is the default/unrecognized status.
	AttemptStatusUnknown
)

// TurnAttempt describes one best-of-N attempt for an assistant turn. It mirrors
// the Rust `TurnAttempt`.
type TurnAttempt struct {
	TurnID           string
	AttemptPlacement *int64
	CreatedAt        *time.Time
	Status           AttemptStatus
	Diff             *string
	Messages         []string
}

// ApplyStatus is the outcome category of an apply/preflight. It mirrors the Rust
// `ApplyStatus` (serde lowercase).
type ApplyStatus string

const (
	// ApplyStatusSuccess indicates the patch applied cleanly.
	ApplyStatusSuccess ApplyStatus = "success"
	// ApplyStatusPartial indicates the patch applied partially.
	ApplyStatusPartial ApplyStatus = "partial"
	// ApplyStatusError indicates the patch failed to apply.
	ApplyStatusError ApplyStatus = "error"
)

// ApplyOutcome is the result of applying or preflighting a task diff. It mirrors
// the Rust `ApplyOutcome`.
type ApplyOutcome struct {
	Applied       bool        `json:"applied"`
	Status        ApplyStatus `json:"status"`
	Message       string      `json:"message"`
	SkippedPaths  []string    `json:"skipped_paths"`
	ConflictPaths []string    `json:"conflict_paths"`
}

// MarshalJSON ensures the slices serialize as [] (not null), matching serde's
// default Vec serialization.
func (o ApplyOutcome) MarshalJSON() ([]byte, error) {
	type alias ApplyOutcome
	clone := alias(o)
	if clone.SkippedPaths == nil {
		clone.SkippedPaths = []string{}
	}
	if clone.ConflictPaths == nil {
		clone.ConflictPaths = []string{}
	}
	return json.Marshal(clone)
}

// CreatedTask is the response of creating a task. It mirrors the Rust
// `CreatedTask`.
type CreatedTask struct {
	ID TaskID `json:"id"`
}

// TaskListPage is a page of task summaries with an optional cursor. It mirrors
// the Rust `TaskListPage`.
type TaskListPage struct {
	Tasks  []TaskSummary
	Cursor *string
}

// DiffSummary aggregates change counts for a task. It mirrors the Rust
// `DiffSummary`.
type DiffSummary struct {
	FilesChanged int `json:"files_changed"`
	LinesAdded   int `json:"lines_added"`
	LinesRemoved int `json:"lines_removed"`
}

// TaskText holds the creating prompt and assistant messages of a task, along
// with attempt metadata. It mirrors the Rust `TaskText`.
type TaskText struct {
	Prompt           *string
	Messages         []string
	TurnID           *string
	SiblingTurnIDs   []string
	AttemptPlacement *int64
	AttemptStatus    AttemptStatus
}

// DefaultTaskText returns the zero-value TaskText, mirroring the Rust
// `TaskText::default` (AttemptStatus defaults to Unknown).
func DefaultTaskText() TaskText {
	return TaskText{AttemptStatus: AttemptStatusUnknown}
}

// CloudTaskError is the error type for cloud-tasks operations. It mirrors the
// Rust `CloudTaskError` enum.
type CloudTaskError struct {
	// Kind categorizes the error.
	Kind CloudTaskErrorKind
	// Detail is the human-readable detail message.
	Detail string
}

// CloudTaskErrorKind enumerates the cloud-task error categories.
type CloudTaskErrorKind int

const (
	// CloudTaskErrorUnimplemented mirrors `CloudTaskError::Unimplemented`.
	CloudTaskErrorUnimplemented CloudTaskErrorKind = iota
	// CloudTaskErrorHTTP mirrors `CloudTaskError::Http`.
	CloudTaskErrorHTTP
	// CloudTaskErrorIO mirrors `CloudTaskError::Io`.
	CloudTaskErrorIO
	// CloudTaskErrorMsg mirrors `CloudTaskError::Msg`.
	CloudTaskErrorMsg
)

// Error implements error, matching the Rust thiserror Display strings.
func (e *CloudTaskError) Error() string {
	switch e.Kind {
	case CloudTaskErrorUnimplemented:
		return fmt.Sprintf("unimplemented: %s", e.Detail)
	case CloudTaskErrorHTTP:
		return fmt.Sprintf("http error: %s", e.Detail)
	case CloudTaskErrorIO:
		return fmt.Sprintf("io error: %s", e.Detail)
	default:
		return e.Detail
	}
}

// newUnimplemented builds a CloudTaskErrorUnimplemented.
func newUnimplemented(detail string) *CloudTaskError {
	return &CloudTaskError{Kind: CloudTaskErrorUnimplemented, Detail: detail}
}

// newHTTPError builds a CloudTaskErrorHTTP.
func newHTTPError(detail string) *CloudTaskError {
	return &CloudTaskError{Kind: CloudTaskErrorHTTP, Detail: detail}
}

// newIOError builds a CloudTaskErrorIO.
func newIOError(detail string) *CloudTaskError {
	return &CloudTaskError{Kind: CloudTaskErrorIO, Detail: detail}
}

// newMsgError builds a CloudTaskErrorMsg.
func newMsgError(detail string) *CloudTaskError {
	return &CloudTaskError{Kind: CloudTaskErrorMsg, Detail: detail}
}
