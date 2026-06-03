package otel

import "testing"

func TestResolveExporterStatsig(t *testing.T) {
	// Not parallel: mutates the debugBuild package var.
	t.Run("release_resolves_to_otlp_http", func(t *testing.T) {
		debugBuild = false
		got := ResolveExporter(StatsigExporter())
		if got.Kind != OtelExporterOtlpHTTP {
			t.Fatalf("expected OTLP HTTP, got kind %d", got.Kind)
		}
		if got.Endpoint != statsigOtlpHTTPEndpoint {
			t.Errorf("endpoint: got %q", got.Endpoint)
		}
		if got.Headers[statsigAPIKeyHeader] != statsigAPIKey {
			t.Errorf("missing statsig api key header")
		}
		if got.Protocol != OtelHTTPProtocolJSON {
			t.Errorf("expected JSON protocol")
		}
	})

	t.Run("debug_resolves_to_none", func(t *testing.T) {
		debugBuild = true
		defer func() { debugBuild = false }()
		got := ResolveExporter(StatsigExporter())
		if got.Kind != OtelExporterNone {
			t.Fatalf("expected None in debug build, got kind %d", got.Kind)
		}
	})

	t.Run("non_statsig_passthrough", func(t *testing.T) {
		in := OtelExporter{Kind: OtelExporterOtlpHTTP, Endpoint: "https://example.com"}
		got := ResolveExporter(in)
		if got.Endpoint != in.Endpoint || got.Kind != in.Kind {
			t.Errorf("non-statsig exporter should pass through unchanged")
		}
	})
}

func TestValidateSpanAttributes(t *testing.T) {
	t.Parallel()
	if err := ValidateSpanAttributes(map[string]string{"k": "v"}); err != nil {
		t.Errorf("valid attributes rejected: %v", err)
	}
	if err := ValidateSpanAttributes(map[string]string{"": "v"}); err == nil {
		t.Error("empty key should be rejected")
	}
}

func TestTargets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		target    string
		logExport bool
		traceSafe bool
	}{
		{"codex_otel.log_only", true, false},
		{"codex_otel.network_proxy", true, false},
		{"codex_otel.trace_safe", false, true},
		{"codex_otel.trace_safe.summary", false, true},
		{"other.target", false, false},
	}
	for _, tt := range tests {
		if got := IsLogExportTarget(tt.target); got != tt.logExport {
			t.Errorf("IsLogExportTarget(%q): got %v want %v", tt.target, got, tt.logExport)
		}
		if got := IsTraceSafeTarget(tt.target); got != tt.traceSafe {
			t.Errorf("IsTraceSafeTarget(%q): got %v want %v", tt.target, got, tt.traceSafe)
		}
	}
}

func TestRuntimeMetricsSummaryMerge(t *testing.T) {
	t.Parallel()
	var s RuntimeMetricsSummary
	if !s.IsEmpty() {
		t.Fatal("zero summary should be empty")
	}
	s.Merge(RuntimeMetricsSummary{
		ToolCalls:              RuntimeMetricTotals{Count: 2, DurationMs: 50},
		ResponsesAPIOverheadMs: 7,
		TurnTtftMs:             3,
	})
	s.Merge(RuntimeMetricsSummary{
		ToolCalls:  RuntimeMetricTotals{Count: 3, DurationMs: 10},
		TurnTtftMs: 9, // latest-wins
	})
	if s.ToolCalls.Count != 5 || s.ToolCalls.DurationMs != 60 {
		t.Errorf("tool calls accumulate: got %+v", s.ToolCalls)
	}
	if s.ResponsesAPIOverheadMs != 7 {
		t.Errorf("overhead preserved: got %d", s.ResponsesAPIOverheadMs)
	}
	if s.TurnTtftMs != 9 {
		t.Errorf("ttft latest-wins: got %d", s.TurnTtftMs)
	}
	if s.IsEmpty() {
		t.Error("populated summary should not be empty")
	}
}
