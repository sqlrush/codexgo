package cli

import (
	"encoding/json"
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
