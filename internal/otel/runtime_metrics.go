package otel

// RuntimeMetricTotals aggregates a count and total duration for one metric
// family. Mirrors Rust `RuntimeMetricTotals`.
type RuntimeMetricTotals struct {
	Count      uint64
	DurationMs uint64
}

// IsEmpty reports whether both totals are zero. Mirrors Rust
// `RuntimeMetricTotals::is_empty`.
func (t RuntimeMetricTotals) IsEmpty() bool {
	return t.Count == 0 && t.DurationMs == 0
}

// Merge saturating-adds another total into this one. Mirrors Rust
// `RuntimeMetricTotals::merge`.
func (t *RuntimeMetricTotals) Merge(other RuntimeMetricTotals) {
	t.Count = saturatingAddU64(t.Count, other.Count)
	t.DurationMs = saturatingAddU64(t.DurationMs, other.DurationMs)
}

// RuntimeMetricsSummary is a snapshot of runtime metric accumulators. Mirrors
// Rust `RuntimeMetricsSummary`.
type RuntimeMetricsSummary struct {
	ToolCalls       RuntimeMetricTotals
	APICalls        RuntimeMetricTotals
	StreamingEvents RuntimeMetricTotals
	WebsocketCalls  RuntimeMetricTotals
	WebsocketEvents RuntimeMetricTotals

	ResponsesAPIOverheadMs          uint64
	ResponsesAPIInferenceTimeMs     uint64
	ResponsesAPIEngineIAPITtftMs    uint64
	ResponsesAPIEngineServiceTtftMs uint64
	ResponsesAPIEngineIAPITbtMs     uint64
	ResponsesAPIEngineServiceTbtMs  uint64

	TurnTtftMs uint64
	TurnTtfmMs uint64
}

// IsEmpty reports whether every accumulator is zero. Mirrors Rust
// `RuntimeMetricsSummary::is_empty`.
func (s RuntimeMetricsSummary) IsEmpty() bool {
	return s.ToolCalls.IsEmpty() &&
		s.APICalls.IsEmpty() &&
		s.StreamingEvents.IsEmpty() &&
		s.WebsocketCalls.IsEmpty() &&
		s.WebsocketEvents.IsEmpty() &&
		s.ResponsesAPIOverheadMs == 0 &&
		s.ResponsesAPIInferenceTimeMs == 0 &&
		s.ResponsesAPIEngineIAPITtftMs == 0 &&
		s.ResponsesAPIEngineServiceTtftMs == 0 &&
		s.ResponsesAPIEngineIAPITbtMs == 0 &&
		s.ResponsesAPIEngineServiceTbtMs == 0 &&
		s.TurnTtftMs == 0 &&
		s.TurnTtfmMs == 0
}

// Merge folds another summary into this one. Counts accumulate; "latest wins"
// semantics apply to the single-value fields. Mirrors Rust
// `RuntimeMetricsSummary::merge`.
func (s *RuntimeMetricsSummary) Merge(other RuntimeMetricsSummary) {
	s.ToolCalls.Merge(other.ToolCalls)
	s.APICalls.Merge(other.APICalls)
	s.StreamingEvents.Merge(other.StreamingEvents)
	s.WebsocketCalls.Merge(other.WebsocketCalls)
	s.WebsocketEvents.Merge(other.WebsocketEvents)
	if other.ResponsesAPIOverheadMs > 0 {
		s.ResponsesAPIOverheadMs = other.ResponsesAPIOverheadMs
	}
	if other.ResponsesAPIInferenceTimeMs > 0 {
		s.ResponsesAPIInferenceTimeMs = other.ResponsesAPIInferenceTimeMs
	}
	if other.ResponsesAPIEngineIAPITtftMs > 0 {
		s.ResponsesAPIEngineIAPITtftMs = other.ResponsesAPIEngineIAPITtftMs
	}
	if other.ResponsesAPIEngineServiceTtftMs > 0 {
		s.ResponsesAPIEngineServiceTtftMs = other.ResponsesAPIEngineServiceTtftMs
	}
	if other.ResponsesAPIEngineIAPITbtMs > 0 {
		s.ResponsesAPIEngineIAPITbtMs = other.ResponsesAPIEngineIAPITbtMs
	}
	if other.ResponsesAPIEngineServiceTbtMs > 0 {
		s.ResponsesAPIEngineServiceTbtMs = other.ResponsesAPIEngineServiceTbtMs
	}
	if other.TurnTtftMs > 0 {
		s.TurnTtftMs = other.TurnTtftMs
	}
	if other.TurnTtfmMs > 0 {
		s.TurnTtfmMs = other.TurnTtfmMs
	}
}

// ResponsesAPISummary extracts only the responses-API timing fields. Mirrors
// Rust `RuntimeMetricsSummary::responses_api_summary`.
func (s RuntimeMetricsSummary) ResponsesAPISummary() RuntimeMetricsSummary {
	return RuntimeMetricsSummary{
		ResponsesAPIOverheadMs:          s.ResponsesAPIOverheadMs,
		ResponsesAPIInferenceTimeMs:     s.ResponsesAPIInferenceTimeMs,
		ResponsesAPIEngineIAPITtftMs:    s.ResponsesAPIEngineIAPITtftMs,
		ResponsesAPIEngineServiceTtftMs: s.ResponsesAPIEngineServiceTtftMs,
		ResponsesAPIEngineIAPITbtMs:     s.ResponsesAPIEngineIAPITbtMs,
		ResponsesAPIEngineServiceTbtMs:  s.ResponsesAPIEngineServiceTbtMs,
	}
}

func saturatingAddU64(a, b uint64) uint64 {
	sum := a + b
	if sum < a {
		return ^uint64(0)
	}
	return sum
}
