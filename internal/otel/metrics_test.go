package otel

import (
	"errors"
	"testing"
)

func TestSanitizeMetricTagValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain", "codex_cli", "codex_cli"},
		{"slashes_allowed", "a/b.c-d_e", "a/b.c-d_e"},
		{"spaces_become_underscore", "a b", "a_b"},
		{"trim_underscores", "__abc__", "abc"},
		{"empty_is_unspecified", "", "unspecified"},
		{"only_symbols_is_unspecified", "@@@", "unspecified"},
		{"unicode_replaced", "café", "caf"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := SanitizeMetricTagValue(tt.input); got != tt.want {
				t.Errorf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestValidateMetricName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid", "codex.tool.call", false},
		{"valid_with_dash", "codex-thing", false},
		{"empty", "", true},
		{"invalid_slash", "a/b", true},
		{"invalid_space", "a b", true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateMetricName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("got err=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateTagComponent(t *testing.T) {
	t.Parallel()
	if err := ValidateTagValue("a/b.c-d_e"); err != nil {
		t.Errorf("slash should be valid in tag value: %v", err)
	}
	if err := ValidateTagKey(""); err == nil {
		t.Error("empty tag key should be invalid")
	}
	if err := ValidateTagValue("a b"); err == nil {
		t.Error("space should be invalid in tag value")
	}
}

func TestBoundedOriginatorTagValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"codex_cli_rs", "codex_cli_rs"},
		{"codex-tui", "codex-tui"},
		{"some_unknown_thing", "other"},
		{"", "other"},
	}
	for _, tt := range tests {
		if got := BoundedOriginatorTagValue(tt.input); got != tt.want {
			t.Errorf("BoundedOriginatorTagValue(%q): got %q want %q", tt.input, got, tt.want)
		}
	}
}

func TestSessionMetricTagValuesOrder(t *testing.T) {
	t.Parallel()
	tags, err := SessionMetricTagValues{
		AuthMode:      ptr("api_key"),
		SessionSource: "cli",
		Originator:    "codex_cli",
		ServiceName:   ptr("desktop_app"),
		Model:         "gpt-5.1",
		AppVersion:    "1.2.3",
	}.IntoTags()
	if err != nil {
		t.Fatalf("into tags: %v", err)
	}
	want := []Tag{
		{AuthModeTag, "api_key"},
		{SessionSourceTag, "cli"},
		{OriginatorTag, "codex_cli"},
		{ServiceNameTag, "desktop_app"},
		{ModelTag, "gpt-5.1"},
		{AppVersionTag, "1.2.3"},
	}
	if len(tags) != len(want) {
		t.Fatalf("len: got %d want %d", len(tags), len(want))
	}
	for i := range want {
		if tags[i] != want[i] {
			t.Errorf("tag %d: got %v want %v", i, tags[i], want[i])
		}
	}
}

func TestSessionMetricTagValuesSkipsOptional(t *testing.T) {
	t.Parallel()
	tags, err := SessionMetricTagValues{
		SessionSource: "exec",
		Originator:    "codex_exec",
		Model:         "gpt-5.1",
		AppVersion:    "1.2.3",
	}.IntoTags()
	if err != nil {
		t.Fatalf("into tags: %v", err)
	}
	want := []Tag{
		{SessionSourceTag, "exec"},
		{OriginatorTag, "codex_exec"},
		{ModelTag, "gpt-5.1"},
		{AppVersionTag, "1.2.3"},
	}
	if len(tags) != len(want) {
		t.Fatalf("len: got %d want %d", len(tags), len(want))
	}
	for i := range want {
		if tags[i] != want[i] {
			t.Errorf("tag %d: got %v want %v", i, tags[i], want[i])
		}
	}
}

func TestMetricsClientCounterValidation(t *testing.T) {
	t.Parallel()
	client, err := NewMetricsClient(OtlpMetricsConfig("test", "codex", "0.0.0", NoneExporter()))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	// Negative increment is rejected.
	err = client.Counter(ToolCallCountMetric, -1, nil)
	var negErr *NegativeCounterIncrementError
	if !errors.As(err, &negErr) {
		t.Fatalf("expected NegativeCounterIncrementError, got %v", err)
	}
	// Empty metric name is rejected.
	if err := client.Counter("", 1, nil); !errors.Is(err, ErrEmptyMetricName) {
		t.Fatalf("expected ErrEmptyMetricName, got %v", err)
	}
	// A valid counter increment succeeds (records into the no-op global meter).
	if err := client.Counter(ToolCallCountMetric, 1, []Tag{{Key: "tool", Value: "shell"}}); err != nil {
		t.Fatalf("valid counter failed: %v", err)
	}
	// Invalid tag value is rejected.
	if err := client.Counter(ToolCallCountMetric, 1, []Tag{{Key: "tool", Value: "bad value"}}); err == nil {
		t.Fatal("expected invalid tag value error")
	}
}
