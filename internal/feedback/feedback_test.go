package feedback

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRingBufferDropsFrontWhenFull(t *testing.T) {
	t.Parallel()
	fb := NewCodexFeedbackWithCapacity(8)
	if _, err := fb.Write([]byte("abcdefgh")); err != nil {
		t.Fatal(err)
	}
	if _, err := fb.Write([]byte("ij")); err != nil {
		t.Fatal(err)
	}
	snap := fb.Snapshot(nil, nil)
	// Capacity 8: after writing 10 bytes, keep the last 8.
	if got := string(snap.AsBytes()); got != "cdefghij" {
		t.Errorf("got %q want %q", got, "cdefghij")
	}
}

func TestRingBufferLargeChunkKeepsTail(t *testing.T) {
	t.Parallel()
	rb := newRingBuffer(4)
	rb.pushBytes([]byte("0123456789"))
	if got := string(rb.snapshotBytes()); got != "6789" {
		t.Errorf("got %q want %q", got, "6789")
	}
}

func TestRecordTagsCapAndSnapshot(t *testing.T) {
	t.Parallel()
	fb := NewCodexFeedback()
	fb.RecordTags(map[string]string{"model": "gpt-5", "cached": "true"})
	snap := fb.Snapshot(nil, nil)
	if snap.tags["model"] != "gpt-5" {
		t.Errorf("model tag: got %q", snap.tags["model"])
	}
	if snap.tags["cached"] != "true" {
		t.Errorf("cached tag: got %q", snap.tags["cached"])
	}
}

func TestRecordTagCapPreventsNewKeys(t *testing.T) {
	t.Parallel()
	fb := NewCodexFeedback()
	for i := 0; i < maxFeedbackTags; i++ {
		fb.RecordTag(string(rune('a'+i%26))+string(rune('0'+i/26)), "v")
	}
	// At the cap, a brand-new key is dropped, but updates to existing keys work.
	fb.RecordTag("brand_new_key", "x")
	fb.RecordTag("a0", "updated")
	snap := fb.Snapshot(nil, nil)
	if _, exists := snap.tags["brand_new_key"]; exists {
		t.Error("new key past cap should be dropped")
	}
	if snap.tags["a0"] != "updated" {
		t.Errorf("existing key should update past cap, got %q", snap.tags["a0"])
	}
}

func TestSnapshotSyntheticThreadID(t *testing.T) {
	t.Parallel()
	fb := NewCodexFeedback()
	snap := fb.Snapshot(nil, func() string { return "abc" })
	if snap.ThreadID != "no-active-thread-abc" {
		t.Errorf("synthetic thread id: got %q", snap.ThreadID)
	}
	sid := "explicit-thread"
	snap2 := fb.Snapshot(&sid, nil)
	if snap2.ThreadID != sid {
		t.Errorf("explicit thread id: got %q", snap2.ThreadID)
	}
}

func TestSaveToTempFile(t *testing.T) {
	t.Parallel()
	fb := NewCodexFeedback()
	_, _ = fb.Write([]byte("hello logs"))
	sid := "save-test"
	snap := fb.Snapshot(&sid, nil)
	path, err := snap.SaveToTempFile()
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	defer os.Remove(path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "hello logs" {
		t.Errorf("temp file content: got %q", string(data))
	}
}

func TestFeedbackAttachmentsOrderAndGating(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	extraPath := filepath.Join(dir, "rollout.jsonl")
	if err := os.WriteFile(extraPath, []byte("rollout"), 0o600); err != nil {
		t.Fatal(err)
	}

	jsonCT := "application/json"
	snap := NewCodexFeedback().Snapshot(nil, func() string { return "t" }).
		WithFeedbackDiagnostics(NewFeedbackDiagnostics([]FeedbackDiagnostic{{
			Headline: "Proxy environment variables are set and may affect connectivity.",
			Details:  []string{"HTTPS_PROXY = https://example.com:443"},
		}}))

	got := snap.feedbackAttachments(
		true,
		[]FeedbackAttachment{{
			Filename:    DoctorReportAttachmentFilename,
			ContentType: &jsonCT,
			Buffer:      []byte(`{"overallStatus":"ok"}`),
		}},
		[]FeedbackAttachmentPath{{Path: extraPath}},
		[]byte{1},
	)

	var names []string
	for _, a := range got {
		names = append(names, a.Filename)
	}
	want := []string{
		"codex-logs.log",
		DoctorReportAttachmentFilename,
		FeedbackDiagnosticsAttachmentFilename,
		"rollout.jsonl",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("attachment order:\n got %v\nwant %v", names, want)
	}
	if string(got[0].Buffer) != string([]byte{1}) {
		t.Errorf("logs override not applied: %v", got[0].Buffer)
	}
	if string(got[2].Buffer) != "Connectivity diagnostics\n\n- Proxy environment variables are set and may affect connectivity.\n  - HTTPS_PROXY = https://example.com:443" {
		t.Errorf("diagnostics text mismatch: %q", string(got[2].Buffer))
	}

	// Without diagnostics, only the logs attachment appears.
	noDiag := NewCodexFeedback().Snapshot(nil, func() string { return "t" }).
		WithFeedbackDiagnostics(FeedbackDiagnostics{}).
		feedbackAttachments(true, nil, nil, []byte{1})
	if len(noDiag) != 1 || noDiag[0].Filename != "codex-logs.log" {
		t.Fatalf("expected only codex-logs.log, got %d attachments", len(noDiag))
	}
}

func TestFeedbackAttachmentsExcludedWhenLogsDisabled(t *testing.T) {
	t.Parallel()
	snap := NewCodexFeedback().Snapshot(nil, func() string { return "t" }).
		WithFeedbackDiagnostics(NewFeedbackDiagnostics([]FeedbackDiagnostic{{Headline: "h"}}))
	// include_logs=false gates both logs and diagnostics.
	got := snap.feedbackAttachments(false, nil, nil, nil)
	if len(got) != 0 {
		t.Fatalf("expected no attachments when logs disabled, got %d", len(got))
	}
}
