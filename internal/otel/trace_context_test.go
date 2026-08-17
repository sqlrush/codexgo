package otel

import (
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func ptr[T any](v T) *T { return &v }

func TestContextFromW3CTraceContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		trace *protocol.W3cTraceContext
		want  bool
	}{
		{
			name: "valid_traceparent",
			trace: &protocol.W3cTraceContext{
				Traceparent: ptr("00-00000000000000000000000000000001-0000000000000002-01"),
			},
			want: true,
		},
		{
			name:  "invalid_traceparent",
			trace: &protocol.W3cTraceContext{Traceparent: ptr("not-a-traceparent")},
			want:  false,
		},
		{
			name:  "missing_traceparent",
			trace: &protocol.W3cTraceContext{Tracestate: ptr("vendor=value")},
			want:  false,
		},
		{
			name:  "nil_trace",
			trace: nil,
			want:  false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, ok := ContextFromW3CTraceContext(tt.trace)
			if ok != tt.want {
				t.Errorf("got ok=%v want %v", ok, tt.want)
			}
		})
	}
}

func TestRoundTripTraceContext(t *testing.T) {
	t.Parallel()
	tp := "00-00000000000000000000000000000abc-000000000000000a-01"
	ctx, ok := ContextFromW3CTraceContext(&protocol.W3cTraceContext{Traceparent: &tp})
	if !ok {
		t.Fatal("expected valid context")
	}
	out := SpanW3CTraceContext(ctx)
	if out == nil || out.Traceparent == nil {
		t.Fatal("expected traceparent to propagate")
	}
	// The trace id portion (bytes 3..35) must be preserved across propagation.
	if (*out.Traceparent)[3:35] != tp[3:35] {
		t.Errorf("trace id not preserved: got %q want trace id %q", *out.Traceparent, tp[3:35])
	}
}

func TestCurrentSpanTraceIDNoSpan(t *testing.T) {
	t.Parallel()
	if id := CurrentSpanTraceID(nil); id != nil {
		t.Errorf("expected nil trace id with no span, got %q", *id)
	}
}

func TestValidateTracestateEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries map[string]map[string]string
		wantErr bool
	}{
		{
			name:    "valid_single_member",
			entries: map[string]map[string]string{"vendor": {"k": "v"}},
			wantErr: false,
		},
		{
			name:    "invalid_field_key_with_separator",
			entries: map[string]map[string]string{"vendor": {"bad;key": "v"}},
			wantErr: true,
		},
		{
			name:    "empty_entries_ok",
			entries: map[string]map[string]string{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateTracestateEntries(tt.entries)
			if (err != nil) != tt.wantErr {
				t.Errorf("got err=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestMergeTracestateMemberFieldsUpsert(t *testing.T) {
	t.Parallel()
	existing := "a:1;b:2"
	merged := mergeTracestateMemberFields(&existing, map[string]string{"b": "9", "c": "3"})
	// b is upserted in place; c is appended; a is preserved.
	want := "a:1;b:9;c:3"
	if merged != want {
		t.Errorf("got %q want %q", merged, want)
	}
}
