package otel

import (
	"errors"
	"fmt"
)

// Sentinel metrics errors. Mirrors the non-source-bearing variants of Rust
// `MetricsError`.
var (
	// ErrEmptyMetricName indicates an empty metric name.
	ErrEmptyMetricName = errors.New("metric name cannot be empty")
	// ErrExporterDisabled indicates the metrics exporter is disabled.
	ErrExporterDisabled = errors.New("metrics exporter is disabled")
	// ErrRuntimeSnapshotUnavailable indicates the runtime reader is not enabled.
	ErrRuntimeSnapshotUnavailable = errors.New("runtime metrics snapshot reader is not enabled")

	errInvalidMetricName = errors.New("metric name contains invalid characters")
)

// NegativeCounterIncrementError indicates a counter was decremented. Mirrors
// the Rust `MetricsError::NegativeCounterIncrement` variant.
type NegativeCounterIncrementError struct {
	Name string
	Inc  int64
}

func (e *NegativeCounterIncrementError) Error() string {
	return fmt.Sprintf("counter increment must be non-negative for %s: %d", e.Name, e.Inc)
}
