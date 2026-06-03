package cliutil

import "strings"

// ResumeCommand builds the user-facing `codex resume` command string for a
// thread, mirroring codex_utils_cli::resume_command.
//
// threadName and threadID are optional (pass nil for absent). The resume target
// is the non-empty thread name when present, otherwise the thread id. When
// neither is available the function returns nil.
//
// The target is shell-quoted exactly as the upstream shlex_join would quote it.
// If the target begins with '-' it is placed after a `--` separator so the
// shell does not interpret it as a flag.
//
// The returned *string (when non-nil) points at freshly allocated storage owned
// by the caller; the inputs are not mutated.
func ResumeCommand(threadName *string, threadID *ThreadID) *string {
	target, ok := resumeTarget(threadName, threadID)
	if !ok {
		return nil
	}

	needsDoubleDash := strings.HasPrefix(target, "-")
	escaped := shlexJoin([]string{target})

	var cmd string
	if needsDoubleDash {
		cmd = "codex resume -- " + escaped
	} else {
		cmd = "codex resume " + escaped
	}
	return &cmd
}

// ResumeHint builds a user-facing hint describing how to resume a thread,
// mirroring codex_utils_cli::resume_hint.
//
// A thread id is required; without one the function returns nil. When a
// non-empty thread name is also present the hint instructs the user to run
// `codex resume` and select the named item (annotated with its id). Otherwise it
// falls back to the direct id-based [ResumeCommand].
//
// The returned *string (when non-nil) points at freshly allocated storage owned
// by the caller; the inputs are not mutated.
func ResumeHint(threadName *string, threadID *ThreadID) *string {
	if threadID == nil || threadID.IsZero() {
		return nil
	}

	if name, ok := nonEmptyName(threadName); ok {
		hint := "codex resume, then select " + name + " (" + threadID.String() + ")"
		return &hint
	}

	return ResumeCommand(nil, threadID)
}

// resumeTarget selects the resume target string, preferring a non-empty thread
// name over the thread id. The bool reports whether a target was found.
func resumeTarget(threadName *string, threadID *ThreadID) (string, bool) {
	if name, ok := nonEmptyName(threadName); ok {
		return name, true
	}
	if threadID != nil && !threadID.IsZero() {
		return threadID.String(), true
	}
	return "", false
}

// nonEmptyName returns the dereferenced thread name when it is present and
// non-empty, mirroring the upstream `.filter(|name| !name.is_empty())`.
func nonEmptyName(threadName *string) (string, bool) {
	if threadName == nil || *threadName == "" {
		return "", false
	}
	return *threadName, true
}
