package cli

import "time"

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
)

// rank orders statuses so the overall status is the worst of all checks.
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

// doctorCheck is one diagnostic result. It mirrors the camelCase DoctorCheck
// struct: a category, a status, a one-line summary, optional details, and an
// optional remediation hint.
type doctorCheck struct {
	ID          string      `json:"id"`
	Category    string      `json:"category"`
	Status      checkStatus `json:"status"`
	Summary     string      `json:"summary"`
	Details     []string    `json:"details"`
	Remediation *string     `json:"remediation"`
	DurationMs  uint64      `json:"durationMs"`
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
