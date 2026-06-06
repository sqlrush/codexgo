package cli

import (
	"encoding/json"
	"time"
)

// doctorSchemaVersion is the schema version of the doctor JSON report. Bumped
// when the report shape changes; mirrors DoctorReport.schema_version.
const doctorSchemaVersion = 1

// checkStatus is the tri-state outcome of a doctor check. It serializes to the
// snake_case strings used by the Rust report (ok, warning, fail).
type checkStatus string

const (
	statusOK      checkStatus = "ok"
	statusWarning checkStatus = "warning"
	statusFail    checkStatus = "fail"
	// statusSkipped marks a check that was intentionally not run (for example a
	// network probe when CODEXGO_DOCTOR_SKIP_NETWORK is set). Skipped checks never
	// affect the overall status.
	statusSkipped checkStatus = "skipped"
)

// rank orders statuses so the overall status is the worst of all checks. Skipped
// checks rank as low as OK so they cannot degrade the overall status.
func (s checkStatus) rank() int {
	switch s {
	case statusFail:
		return 2
	case statusWarning:
		return 1
	default:
		return 0
	}
}

// doctorReport is the machine-readable report shared by the human and JSON
// renderers. It mirrors the camelCase DoctorReport struct.
type doctorReport struct {
	SchemaVersion uint32        `json:"schemaVersion"`
	GeneratedAt   string        `json:"generatedAt"`
	OverallStatus checkStatus   `json:"overallStatus"`
	CodexVersion  string        `json:"codexVersion"`
	Checks        []doctorCheck `json:"checks"`
}

// MarshalJSON renders the report with `checks` as a JSON object keyed by check
// id, matching codex 0.136.0 (whose checks map serializes in sorted-key order,
// which Go's encoding/json reproduces for map[string]). The internal slice is
// retained for ordered human rendering and overall-status computation.
func (r doctorReport) MarshalJSON() ([]byte, error) {
	byID := make(map[string]doctorCheck, len(r.Checks))
	for _, c := range r.Checks {
		byID[c.ID] = c
	}
	return json.Marshal(struct {
		SchemaVersion uint32                 `json:"schemaVersion"`
		GeneratedAt   string                 `json:"generatedAt"`
		OverallStatus checkStatus            `json:"overallStatus"`
		CodexVersion  string                 `json:"codexVersion"`
		Checks        map[string]doctorCheck `json:"checks"`
	}{r.SchemaVersion, r.GeneratedAt, r.OverallStatus, r.CodexVersion, byID})
}

// doctorCheck is one diagnostic result. It mirrors the camelCase DoctorCheck
// struct: a category, a status, a one-line summary, optional details, and an
// optional remediation hint.
//
// Details are stored internally as ordered "label: value" strings (matching the
// Rust DoctorCheck.details: Vec<String>) so the human renderer can parse them by
// label; MarshalJSON converts them into the keyed object shape codex emits.
type doctorCheck struct {
	ID          string      `json:"id"`
	Category    string      `json:"category"`
	Status      checkStatus `json:"status"`
	Summary     string      `json:"summary"`
	Details     []string    `json:"-"`
	Remediation *string     `json:"remediation"`
	DurationMs  uint64      `json:"durationMs"`
}

// MarshalJSON renders the redacted JSON check shape from doctor.rs: details are
// emitted as an object keyed by detail label (a string for single values, an
// array for repeated labels), and non-conforming detail strings are surfaced as
// `notes` (omitted when empty), mirroring JsonDoctorCheck/structured_json_details.
//
// An empty details set serializes to `{}` (never null), matching codex's
// `mcp.config` row whose `details` is an empty object.
func (c doctorCheck) MarshalJSON() ([]byte, error) {
	details, notes := structuredJSONDetails(c.Details)
	return json.Marshal(struct {
		ID          string                     `json:"id"`
		Category    string                     `json:"category"`
		Status      checkStatus                `json:"status"`
		Summary     string                     `json:"summary"`
		Details     map[string]jsonDetailValue `json:"details"`
		Notes       []string                   `json:"notes,omitempty"`
		Remediation *string                    `json:"remediation"`
		DurationMs  uint64                     `json:"durationMs"`
	}{
		ID:          c.ID,
		Category:    c.Category,
		Status:      c.Status,
		Summary:     c.Summary,
		Details:     details,
		Notes:       notes,
		Remediation: c.Remediation,
		DurationMs:  c.DurationMs,
	})
}

// newDoctorReport assembles a report from the given checks, computing the overall
// status as the worst individual status (ok < warning < fail).
func newDoctorReport(version string, checks []doctorCheck, now time.Time) doctorReport {
	overall := statusOK
	for _, c := range checks {
		if c.Status.rank() > overall.rank() {
			overall = c.Status
		}
	}
	return doctorReport{
		SchemaVersion: doctorSchemaVersion,
		GeneratedAt:   now.UTC().Format(time.RFC3339),
		OverallStatus: overall,
		CodexVersion:  version,
		Checks:        checks,
	}
}

// checkBuilder accumulates a single doctor check result.
type checkBuilder struct {
	id          string
	category    string
	status      checkStatus
	summary     string
	details     []string
	remediation *string
	started     time.Time
}

// newCheck starts a check timer for the given id/category.
func newCheck(id, category string) *checkBuilder {
	return &checkBuilder{id: id, category: category, status: statusOK, started: time.Now()}
}

// ok sets the check to OK with the given summary.
func (b *checkBuilder) ok(summary string) *checkBuilder {
	b.status = statusOK
	b.summary = summary
	return b
}

// warn sets the check to warning with the given summary.
func (b *checkBuilder) warn(summary string) *checkBuilder {
	b.status = statusWarning
	b.summary = summary
	return b
}

// fail sets the check to fail with the given summary.
func (b *checkBuilder) fail(summary string) *checkBuilder {
	b.status = statusFail
	b.summary = summary
	return b
}

// skipped sets the check to skipped with the given summary. Skipped checks are
// used when a probe is intentionally not run (for example offline runs).
func (b *checkBuilder) skipped(summary string) *checkBuilder {
	b.status = statusSkipped
	b.summary = summary
	return b
}

// detail appends a detail line.
func (b *checkBuilder) detail(line string) *checkBuilder {
	b.details = append(b.details, line)
	return b
}

// remedy sets the remediation hint.
func (b *checkBuilder) remedy(remediation string) *checkBuilder {
	b.remediation = &remediation
	return b
}

// build finalizes the check, recording its elapsed duration.
func (b *checkBuilder) build() doctorCheck {
	return doctorCheck{
		ID:          b.id,
		Category:    b.category,
		Status:      b.status,
		Summary:     b.summary,
		Details:     b.details,
		Remediation: b.remediation,
		DurationMs:  uint64(time.Since(b.started).Milliseconds()),
	}
}
