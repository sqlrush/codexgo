package core

import (
	"strings"
	"testing"
)

// TestRenderEnvironmentContextReadOnly pins the `<environment_context>` block
// codex emits for a single-environment read-only turn — matching the structure
// captured byte-for-byte from the real codex 0.136.0 binary's /responses request
// (with fixed cwd/shell/date/timezone so the test is deterministic).
func TestRenderEnvironmentContextReadOnly(t *testing.T) {
	const cwd = "/work/project"
	in := EnvironmentContextInput{
		Cwd:         cwd,
		Shell:       "zsh",
		CurrentDate: "2026-06-04",
		Timezone:    "Asia/Shanghai",
		Filesystem:  ReadOnlyFilesystemContext(cwd),
	}

	const want = "<environment_context>\n" +
		"  <cwd>/work/project</cwd>\n" +
		"  <shell>zsh</shell>\n" +
		"  <current_date>2026-06-04</current_date>\n" +
		"  <timezone>Asia/Shanghai</timezone>\n" +
		`  <filesystem><workspace_roots><root>/work/project</root></workspace_roots>` +
		`<permission_profile type="managed"><file_system type="restricted">` +
		`<entry access="read"><special>:root</special></entry></file_system>` +
		`</permission_profile></filesystem>` + "\n" +
		"</environment_context>"

	got := RenderEnvironmentContext(in)
	if got != want {
		t.Errorf("environment_context mismatch\n want: %q\n got:  %q", want, got)
	}
}

// TestRenderEnvironmentContextEscapesPaths verifies the filesystem root path is
// XML-escaped via push_text_element (cwd/shell lines use raw format, matching
// codex), so an angle bracket in a root is encoded.
func TestRenderEnvironmentContextEscapesPaths(t *testing.T) {
	in := EnvironmentContextInput{
		Cwd:        "/a&b",
		Shell:      "zsh",
		Filesystem: ReadOnlyFilesystemContext("/a&b"),
	}
	got := RenderEnvironmentContext(in)
	// cwd line is raw (matches codex format!): contains the literal &.
	if !strings.Contains(got, "<cwd>/a&b</cwd>") {
		t.Errorf("cwd line should be raw; got %q", got)
	}
	// filesystem root is escaped via push_text_element.
	if !strings.Contains(got, "<root>/a&amp;b</root>") {
		t.Errorf("filesystem root should be XML-escaped; got %q", got)
	}
}
