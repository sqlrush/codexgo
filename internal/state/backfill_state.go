package state

import (
	"fmt"
	"time"
)

// BackfillStatus is the lifecycle status of rollout metadata backfill.
type BackfillStatus int

const (
	// BackfillStatusPending means backfill has not started.
	BackfillStatusPending BackfillStatus = iota
	// BackfillStatusRunning means a worker is actively backfilling.
	BackfillStatusRunning
	// BackfillStatusComplete means backfill finished.
	BackfillStatusComplete
)

// String returns the stored textual representation, matching Rust `as_str`.
func (s BackfillStatus) String() string {
	switch s {
	case BackfillStatusPending:
		return "pending"
	case BackfillStatusRunning:
		return "running"
	case BackfillStatusComplete:
		return "complete"
	default:
		return "pending"
	}
}

// ParseBackfillStatus parses a stored backfill status string.
func ParseBackfillStatus(value string) (BackfillStatus, error) {
	switch value {
	case "pending":
		return BackfillStatusPending, nil
	case "running":
		return BackfillStatusRunning, nil
	case "complete":
		return BackfillStatusComplete, nil
	default:
		return 0, fmt.Errorf("invalid backfill status: %q", value)
	}
}

// BackfillState is the persisted lifecycle state for rollout metadata backfill.
type BackfillState struct {
	// Status is the current lifecycle status.
	Status BackfillStatus
	// LastWatermark is the last processed rollout watermark, if any.
	LastWatermark *string
	// LastSuccessAt is the last successful completion time, if any.
	LastSuccessAt *time.Time
}

// DefaultBackfillState returns the zero/pending backfill state.
func DefaultBackfillState() BackfillState {
	return BackfillState{Status: BackfillStatusPending}
}
