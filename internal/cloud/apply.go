package cloud

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/internal/gitutils"
)

// nowRFC3339 returns the current UTC time formatted like Rust's
// chrono::Utc::now().to_rfc3339().
func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// ApplyTask applies a task's diff to the working tree.
func (c *HTTPClient) ApplyTask(ctx context.Context, id TaskID, diffOverride *string) (ApplyOutcome, error) {
	return c.runApply(ctx, id, diffOverride, false)
}

// ApplyTaskPreflight validates whether the patch would apply cleanly.
func (c *HTTPClient) ApplyTaskPreflight(ctx context.Context, id TaskID, diffOverride *string) (ApplyOutcome, error) {
	return c.runApply(ctx, id, diffOverride, true)
}

// runApply mirrors the Rust `Apply::run`.
func (c *HTTPClient) runApply(ctx context.Context, taskID TaskID, diffOverride *string, preflight bool) (ApplyOutcome, error) {
	id := string(taskID)
	var diff string
	if diffOverride != nil {
		diff = *diffOverride
	} else {
		details, err := c.backend.GetTaskDetails(ctx, id)
		if err != nil {
			return ApplyOutcome{}, newHTTPError(fmt.Sprintf("get_task_details failed: %v", err))
		}
		d, ok := details.UnifiedDiff()
		if !ok {
			return ApplyOutcome{}, newMsgError(fmt.Sprintf("No diff available for task %s", id))
		}
		diff = d
	}

	if !isUnifiedDiff(diff) {
		summary := summarizePatchForLogging(diff)
		mode := applyMode(preflight)
		appendErrorLog(fmt.Sprintf("apply_error: id=%s mode=%s format=non-unified; %s", id, mode, summary))
		return ApplyOutcome{
			Applied:       false,
			Status:        ApplyStatusError,
			Message:       "Expected unified git diff; backend returned an incompatible format.",
			SkippedPaths:  nil,
			ConflictPaths: nil,
		}, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = os.TempDir()
	}
	r, err := gitutils.ApplyGitPatch(gitutils.ApplyGitRequest{
		Cwd:       cwd,
		Diff:      diff,
		Revert:    false,
		Preflight: preflight,
	})
	if err != nil {
		return ApplyOutcome{}, newIOError(fmt.Sprintf("git apply failed to run: %v", err))
	}

	status := applyStatusFromResult(r)
	applied := status == ApplyStatusSuccess && !preflight
	message := applyMessage(id, preflight, status, r)

	if status == ApplyStatusPartial || status == ApplyStatusError || (preflight && status != ApplyStatusSuccess) {
		appendErrorLog(applyResultLog(id, preflight, status, r, diff))
	}

	return ApplyOutcome{
		Applied:       applied,
		Status:        status,
		Message:       message,
		SkippedPaths:  r.SkippedPaths,
		ConflictPaths: r.ConflictedPaths,
	}, nil
}

func applyStatusFromResult(r gitutils.ApplyGitResult) ApplyStatus {
	switch {
	case r.ExitCode == 0:
		return ApplyStatusSuccess
	case len(r.AppliedPaths) > 0 || len(r.ConflictedPaths) > 0:
		return ApplyStatusPartial
	default:
		return ApplyStatusError
	}
}

func applyMode(preflight bool) string {
	if preflight {
		return "preflight"
	}
	return "apply"
}

func applyMessage(id string, preflight bool, status ApplyStatus, r gitutils.ApplyGitResult) string {
	if preflight {
		switch status {
		case ApplyStatusSuccess:
			return fmt.Sprintf("Preflight passed for task %s (applies cleanly)", id)
		case ApplyStatusPartial:
			return fmt.Sprintf("Preflight: patch does not fully apply for task %s (applied=%d, skipped=%d, conflicts=%d)",
				id, len(r.AppliedPaths), len(r.SkippedPaths), len(r.ConflictedPaths))
		default:
			return fmt.Sprintf("Preflight failed for task %s (applied=%d, skipped=%d, conflicts=%d)",
				id, len(r.AppliedPaths), len(r.SkippedPaths), len(r.ConflictedPaths))
		}
	}
	switch status {
	case ApplyStatusSuccess:
		return fmt.Sprintf("Applied task %s locally (%d files)", id, len(r.AppliedPaths))
	case ApplyStatusPartial:
		return fmt.Sprintf("Apply partially succeeded for task %s (applied=%d, skipped=%d, conflicts=%d)",
			id, len(r.AppliedPaths), len(r.SkippedPaths), len(r.ConflictedPaths))
	default:
		return fmt.Sprintf("Apply failed for task %s (applied=%d, skipped=%d, conflicts=%d)",
			id, len(r.AppliedPaths), len(r.SkippedPaths), len(r.ConflictedPaths))
	}
}

func applyStatusDebug(status ApplyStatus) string {
	switch status {
	case ApplyStatusSuccess:
		return "Success"
	case ApplyStatusPartial:
		return "Partial"
	default:
		return "Error"
	}
}

func applyResultLog(id string, preflight bool, status ApplyStatus, r gitutils.ApplyGitResult, diff string) string {
	var b strings.Builder
	mode := applyMode(preflight)
	fmt.Fprintf(&b, "apply_result: mode=%s id=%s status=%s applied=%d skipped=%d conflicts=%d cmd=%s\n",
		mode, id, applyStatusDebug(status), len(r.AppliedPaths), len(r.SkippedPaths), len(r.ConflictedPaths), r.CmdForLog)
	fmt.Fprintf(&b, "stdout_tail=\n%s\nstderr_tail=\n%s\n", tail(r.Stdout, 2000), tail(r.Stderr, 2000))
	fmt.Fprintf(&b, "%s\n", summarizePatchForLogging(diff))
	fmt.Fprintf(&b, "----- PATCH BEGIN -----\n%s\n----- PATCH END -----\n", diff)
	return b.String()
}

func tail(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[len(s)-max:]
}

// summarizePatchForLogging mirrors the Rust `summarize_patch_for_logging`.
func summarizePatchForLogging(patch string) string {
	trimmed := strings.TrimLeft(patch, " \t\r\n")
	var kind string
	switch {
	case strings.HasPrefix(trimmed, "*** Begin Patch"):
		kind = "codex-patch"
	case strings.HasPrefix(trimmed, "diff --git ") || strings.Contains(trimmed, "\n*** End Patch\n"):
		kind = "git-diff"
	case strings.HasPrefix(trimmed, "@@ ") || strings.Contains(trimmed, "\n@@ "):
		kind = "unified-diff"
	default:
		kind = "unknown"
	}
	chars := len(patch)
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "<unknown>"
	}
	headLines := strings.Split(patch, "\n")
	if len(headLines) > 20 {
		headLines = headLines[:20]
	}
	head := strings.Join(headLines, "\n")
	if len(head) > 800 {
		head = head[:800] + "…"
	}
	return fmt.Sprintf("patch_summary: kind=%s lines=%d chars=%d cwd=%s ; head=\n%s", kind, lineCount(patch), chars, cwd, head)
}

// lineCount mirrors Rust str::lines().count() (no trailing empty for final \n).
func lineCount(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// appendErrorLog appends a timestamped line to error.log in the cwd. It mirrors
// the Rust `append_error_log`.
func appendErrorLog(message string) {
	f, err := os.OpenFile("error.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	ts := nowRFC3339()
	fmt.Fprintf(f, "[%s] %s\n", ts, message)
}
