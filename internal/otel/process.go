package otel

import "sync/atomic"

// processStartRecorded ensures the process-start counter is emitted at most once
// per process. Mirrors the PROCESS_START_RECORDED AtomicBool in codex.
var processStartRecorded atomic.Bool

// RecordProcessStartOnce records the process start counter at most once. Returns
// true if the counter was recorded by this call. Mirrors Rust
// `record_process_start_once`.
func RecordProcessStartOnce(metrics *MetricsClient, originator string) (bool, error) {
	if !processStartRecorded.CompareAndSwap(false, true) {
		return false, nil
	}
	if err := metrics.Counter(
		ProcessStartMetric,
		1,
		[]Tag{{Key: OriginatorTag, Value: BoundedOriginatorTagValue(originator)}},
	); err != nil {
		return false, err
	}
	return true, nil
}
