package unifiedexec

import (
	"strings"
	"testing"
)

func TestErrorMessages(t *testing.T) {
	tests := []struct {
		name    string
		err     *Error
		wantSub string
	}{
		{"create process", newCreateProcessError("nope"), "Failed to create unified exec process: nope"},
		{"process failed", newProcessFailedError("oops"), "Unified exec process failed: oops"},
		{"unknown process id", newUnknownProcessIDError(123), "Unknown process id 123"},
		{"write to stdin", newWriteToStdinError(), "failed to write to stdin"},
		{"stdin closed", newStdinClosedError(), "stdin is closed for this session"},
		{"missing command line", newMissingCommandLineError(), "missing command line"},
		{"sandbox denied", newSandboxDeniedError("blocked", DeniedOutput{ExitCode: 1}), "Command denied by sandbox: blocked"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); !strings.Contains(got, tt.wantSub) {
				t.Fatalf("Error() = %q, want substring %q", got, tt.wantSub)
			}
		})
	}
}

func TestSandboxDeniedCarriesOutput(t *testing.T) {
	err := newSandboxDeniedError("blocked", DeniedOutput{ExitCode: 42, AggregatedText: "stderr text"})
	if err.Output == nil {
		t.Fatalf("Output = nil, want non-nil")
	}
	if err.Output.ExitCode != 42 {
		t.Fatalf("ExitCode = %d, want 42", err.Output.ExitCode)
	}
	if err.Output.AggregatedText != "stderr text" {
		t.Fatalf("AggregatedText = %q, want %q", err.Output.AggregatedText, "stderr text")
	}
}
