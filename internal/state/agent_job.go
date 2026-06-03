package state

import (
	"encoding/json"
	"fmt"
	"time"
)

// AgentJobStatus is the lifecycle status of an agent job.
type AgentJobStatus int

const (
	// AgentJobStatusPending means the job has not started.
	AgentJobStatusPending AgentJobStatus = iota
	// AgentJobStatusRunning means the job is executing.
	AgentJobStatusRunning
	// AgentJobStatusCompleted means the job finished successfully.
	AgentJobStatusCompleted
	// AgentJobStatusFailed means the job failed.
	AgentJobStatusFailed
	// AgentJobStatusCancelled means the job was cancelled.
	AgentJobStatusCancelled
)

// String returns the stored textual representation, matching Rust `as_str`.
func (s AgentJobStatus) String() string {
	switch s {
	case AgentJobStatusPending:
		return "pending"
	case AgentJobStatusRunning:
		return "running"
	case AgentJobStatusCompleted:
		return "completed"
	case AgentJobStatusFailed:
		return "failed"
	case AgentJobStatusCancelled:
		return "cancelled"
	default:
		return "pending"
	}
}

// ParseAgentJobStatus parses a stored agent job status string.
func ParseAgentJobStatus(value string) (AgentJobStatus, error) {
	switch value {
	case "pending":
		return AgentJobStatusPending, nil
	case "running":
		return AgentJobStatusRunning, nil
	case "completed":
		return AgentJobStatusCompleted, nil
	case "failed":
		return AgentJobStatusFailed, nil
	case "cancelled":
		return AgentJobStatusCancelled, nil
	default:
		return 0, fmt.Errorf("invalid agent job status: %q", value)
	}
}

// IsFinal reports whether the status is terminal.
func (s AgentJobStatus) IsFinal() bool {
	return s == AgentJobStatusCompleted || s == AgentJobStatusFailed || s == AgentJobStatusCancelled
}

// AgentJobItemStatus is the lifecycle status of a single agent job item.
type AgentJobItemStatus int

const (
	// AgentJobItemStatusPending means the item has not started.
	AgentJobItemStatusPending AgentJobItemStatus = iota
	// AgentJobItemStatusRunning means the item is executing.
	AgentJobItemStatusRunning
	// AgentJobItemStatusCompleted means the item finished successfully.
	AgentJobItemStatusCompleted
	// AgentJobItemStatusFailed means the item failed.
	AgentJobItemStatusFailed
)

// String returns the stored textual representation, matching Rust `as_str`.
func (s AgentJobItemStatus) String() string {
	switch s {
	case AgentJobItemStatusPending:
		return "pending"
	case AgentJobItemStatusRunning:
		return "running"
	case AgentJobItemStatusCompleted:
		return "completed"
	case AgentJobItemStatusFailed:
		return "failed"
	default:
		return "pending"
	}
}

// ParseAgentJobItemStatus parses a stored agent job item status string.
func ParseAgentJobItemStatus(value string) (AgentJobItemStatus, error) {
	switch value {
	case "pending":
		return AgentJobItemStatusPending, nil
	case "running":
		return AgentJobItemStatusRunning, nil
	case "completed":
		return AgentJobItemStatusCompleted, nil
	case "failed":
		return AgentJobItemStatusFailed, nil
	default:
		return 0, fmt.Errorf("invalid agent job item status: %q", value)
	}
}

// AgentJob is a batch agent job over a CSV input.
type AgentJob struct {
	ID                string
	Name              string
	Status            AgentJobStatus
	Instruction       string
	AutoExport        bool
	MaxRuntimeSeconds *uint64
	OutputSchemaJSON  json.RawMessage
	InputHeaders      []string
	InputCsvPath      string
	OutputCsvPath     string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	StartedAt         *time.Time
	CompletedAt       *time.Time
	LastError         *string
}

// AgentJobItem is a single row of an agent job's CSV input.
type AgentJobItem struct {
	JobID            string
	ItemID           string
	RowIndex         int64
	SourceID         *string
	RowJSON          json.RawMessage
	Status           AgentJobItemStatus
	AssignedThreadID *string
	AttemptCount     int64
	ResultJSON       json.RawMessage
	LastError        *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CompletedAt      *time.Time
	ReportedAt       *time.Time
}

// AgentJobProgress summarizes the item-status counts of a job.
type AgentJobProgress struct {
	TotalItems     int
	PendingItems   int
	RunningItems   int
	CompletedItems int
	FailedItems    int
}

// AgentJobCreateParams are the inputs to create a new agent job.
type AgentJobCreateParams struct {
	ID                string
	Name              string
	Instruction       string
	AutoExport        bool
	MaxRuntimeSeconds *uint64
	OutputSchemaJSON  json.RawMessage
	InputHeaders      []string
	InputCsvPath      string
	OutputCsvPath     string
}

// AgentJobItemCreateParams are the inputs to create a new agent job item.
type AgentJobItemCreateParams struct {
	ItemID   string
	RowIndex int64
	SourceID *string
	RowJSON  json.RawMessage
}
