package ollama

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func u64(v uint64) *uint64 { return &v }

func TestCliReporterStatusSuppressesManifest(t *testing.T) {
	var buf bytes.Buffer
	r := NewCliProgressReporterWithWriter(&buf)
	if err := r.OnEvent(NewPullStatus("pulling manifest")); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output for manifest status, got %q", buf.String())
	}

	if err := r.OnEvent(NewPullStatus("verifying")); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	if !strings.Contains(buf.String(), "verifying") {
		t.Errorf("expected verifying in output, got %q", buf.String())
	}
}

func TestCliReporterChunkProgressHeaderAndLine(t *testing.T) {
	var buf bytes.Buffer
	r := NewCliProgressReporterWithWriter(&buf)
	// Pin time so the speed estimate is deterministic.
	base := time.Unix(0, 0)
	r.lastInstant = base
	r.now = func() time.Time { return base.Add(time.Second) }

	gib := uint64(1024 * 1024 * 1024)
	if err := r.OnEvent(NewPullChunkProgress("sha256:a", u64(2*gib), u64(gib))); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Downloading model: total 2.00 GB") {
		t.Errorf("missing header in output: %q", out)
	}
	if !strings.Contains(out, "1.00/2.00 GB (50.0%)") {
		t.Errorf("missing progress line in output: %q", out)
	}
	// 1 GiB transferred over 1s -> 1024.0 MB/s.
	if !strings.Contains(out, "1024.0 MB/s") {
		t.Errorf("missing speed in output: %q", out)
	}
}

func TestCliReporterChunkProgressZeroTotalNoOutput(t *testing.T) {
	var buf bytes.Buffer
	r := NewCliProgressReporterWithWriter(&buf)
	if err := r.OnEvent(NewPullChunkProgress("sha256:a", nil, u64(10))); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output when sum_total is 0, got %q", buf.String())
	}
}

func TestCliReporterSuccessWritesNewline(t *testing.T) {
	var buf bytes.Buffer
	r := NewCliProgressReporterWithWriter(&buf)
	if err := r.OnEvent(NewPullSuccess()); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	if buf.String() != "\n" {
		t.Errorf("success output = %q, want newline", buf.String())
	}
}

func TestCliReporterErrorIsSilent(t *testing.T) {
	var buf bytes.Buffer
	r := NewCliProgressReporterWithWriter(&buf)
	if err := r.OnEvent(NewPullError("boom")); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("error event should be silent, got %q", buf.String())
	}
}

func TestTuiReporterDelegates(t *testing.T) {
	var buf bytes.Buffer
	inner := NewCliProgressReporterWithWriter(&buf)
	r := &TuiProgressReporter{inner: inner}
	if err := r.OnEvent(NewPullStatus("verifying")); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	if !strings.Contains(buf.String(), "verifying") {
		t.Errorf("TUI reporter did not delegate; output %q", buf.String())
	}
}
