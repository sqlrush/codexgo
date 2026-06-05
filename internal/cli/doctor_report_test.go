package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNewDoctorReportOverallStatus(t *testing.T) {
	tests := []struct {
		name     string
		statuses []checkStatus
		want     checkStatus
	}{
		{name: "all ok", statuses: []checkStatus{statusOK, statusOK}, want: statusOK},
		{name: "warning wins over ok", statuses: []checkStatus{statusOK, statusWarning}, want: statusWarning},
		{name: "fail wins over warning", statuses: []checkStatus{statusWarning, statusFail}, want: statusFail},
		{name: "empty", statuses: nil, want: statusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checks := make([]doctorCheck, 0, len(tt.statuses))
			for i, s := range tt.statuses {
				checks = append(checks, doctorCheck{ID: "c", Status: s, Category: "x", Details: []string{}})
				_ = i
			}
			report := newDoctorReport("1.2.3", checks, time.Unix(0, 0))
			if report.OverallStatus != tt.want {
				t.Errorf("overall = %q, want %q", report.OverallStatus, tt.want)
			}
			if report.CodexVersion != "1.2.3" {
				t.Errorf("version = %q", report.CodexVersion)
			}
			if report.SchemaVersion != doctorSchemaVersion {
				t.Errorf("schema version = %d, want %d", report.SchemaVersion, doctorSchemaVersion)
			}
		})
	}
}

func TestDoctorReportJSONShape(t *testing.T) {
	report := newDoctorReport("0.0.0", []doctorCheck{
		newCheck("system", "system").ok("good").detail("os: test").build(),
	}, time.Unix(0, 0))

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"schemaVersion", "generatedAt", "overallStatus", "codexVersion", "checks"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("report JSON missing key %q", key)
		}
	}
}

func TestCheckBuilderTransitions(t *testing.T) {
	c := newCheck("auth", "auth").warn("not logged in").remedy("run codex login").build()
	if c.Status != statusWarning {
		t.Errorf("status = %q, want warning", c.Status)
	}
	if c.Remediation == nil || *c.Remediation != "run codex login" {
		t.Errorf("remediation = %v", c.Remediation)
	}
}

// TestStructuredJSONDetails mirrors structured_json_details in doctor.rs: each
// "label: value" detail becomes a map entry keyed by the label, repeated labels
// collapse into an ordered array, and detail strings without a ": " separator are
// dropped from the map (preserved as notes).
func TestStructuredJSONDetails(t *testing.T) {
	details := []string{
		"model: gpt-5.5",
		"config.toml: /home/.codex/config.toml",
		"config.toml: missing",
		"a free-form note without a separator",
		": value with empty key",
	}
	structured, notes := structuredJSONDetails(details)

	if got, ok := structured["model"]; !ok || got.One == nil || *got.One != "gpt-5.5" {
		t.Errorf("model = %#v, want One=gpt-5.5", got)
	}
	got := structured["config.toml"]
	if got.One != nil {
		t.Errorf("config.toml should be Many, got One=%q", *got.One)
	}
	if want := []string{"/home/.codex/config.toml", "missing"}; len(got.Many) != 2 || got.Many[0] != want[0] || got.Many[1] != want[1] {
		t.Errorf("config.toml = %#v, want %v", got.Many, want)
	}
	if len(notes) != 2 {
		t.Errorf("notes = %v, want 2 entries", notes)
	}
}

// TestDoctorCheckDetailsMarshalAsObject asserts the JSON `details` field is an
// object (map of label -> string|[]string), matching codex 0.136.0. The empty
// case serializes to {} (not null) so callers can index into it safely.
func TestDoctorCheckDetailsMarshalAsObject(t *testing.T) {
	check := newCheck("config.load", "config").
		ok("config loaded").
		detail("model: gpt-5.5").
		detail("config.toml: /home/.codex/config.toml").
		detail("config.toml: missing").
		build()

	data, err := json.Marshal(check)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded struct {
		Details map[string]json.RawMessage `json:"details"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(decoded.Details["model"]) != `"gpt-5.5"` {
		t.Errorf("model = %s, want scalar string", decoded.Details["model"])
	}
	if string(decoded.Details["config.toml"]) != `["/home/.codex/config.toml","missing"]` {
		t.Errorf("config.toml = %s, want array", decoded.Details["config.toml"])
	}
}

// TestEmptyDetailsMarshalAsEmptyObject asserts a check with no details serializes
// details as {} rather than null, matching codex's mcp.config "details": {}.
func TestEmptyDetailsMarshalAsEmptyObject(t *testing.T) {
	check := newCheck("mcp.config", "mcp").ok("no MCP servers configured").build()
	data, err := json.Marshal(check)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"details":{}`) {
		t.Errorf("empty details = %s, want \"details\":{}", data)
	}
}
