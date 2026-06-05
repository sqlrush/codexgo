package core

import (
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
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

// TestRenderEnvironmentContextWorkspaceWrite pins the `<environment_context>`
// filesystem block codex emits for a single-environment workspace-write turn,
// captured byte-for-byte from the real codex 0.136.0 binary's /responses request
// (sandbox_mode = "workspace-write", no extra writable_roots). The managed
// restricted profile carries: :root read, the materialized project root (cwd)
// write, :slash_tmp / :tmpdir write, and the default read-only carveouts
// {cwd}/.git, {cwd}/.agents, {cwd}/.codex.
func TestRenderEnvironmentContextWorkspaceWrite(t *testing.T) {
	const cwd = "/work/project"
	in := EnvironmentContextInput{
		Cwd:         cwd,
		Shell:       "zsh",
		CurrentDate: "2026-06-05",
		Timezone:    "GMT",
		Filesystem:  WorkspaceWriteFilesystemContext(cwd),
	}

	const want = "<environment_context>\n" +
		"  <cwd>/work/project</cwd>\n" +
		"  <shell>zsh</shell>\n" +
		"  <current_date>2026-06-05</current_date>\n" +
		"  <timezone>GMT</timezone>\n" +
		`  <filesystem><workspace_roots><root>/work/project</root></workspace_roots>` +
		`<permission_profile type="managed"><file_system type="restricted">` +
		`<entry access="read"><special>:root</special></entry>` +
		`<entry access="write"><path>/work/project</path></entry>` +
		`<entry access="write"><special>:slash_tmp</special></entry>` +
		`<entry access="write"><special>:tmpdir</special></entry>` +
		`<entry access="read"><path>/work/project/.git</path></entry>` +
		`<entry access="read"><path>/work/project/.agents</path></entry>` +
		`<entry access="read"><path>/work/project/.codex</path></entry>` +
		`</file_system></permission_profile></filesystem>` + "\n" +
		"</environment_context>"

	got := RenderEnvironmentContext(in)
	if got != want {
		t.Errorf("workspace-write environment_context mismatch\n want: %q\n got:  %q", want, got)
	}
}

// TestRenderEnvironmentContextDangerFullAccess pins the
// `<environment_context>` filesystem block codex emits for a danger-full-access
// turn, captured byte-for-byte (sandbox_mode = "danger-full-access"): a disabled
// permission profile with an unrestricted file system.
func TestRenderEnvironmentContextDangerFullAccess(t *testing.T) {
	const cwd = "/work/project"
	in := EnvironmentContextInput{
		Cwd:         cwd,
		Shell:       "zsh",
		CurrentDate: "2026-06-05",
		Timezone:    "GMT",
		Filesystem:  DangerFullAccessFilesystemContext(cwd),
	}

	const want = "<environment_context>\n" +
		"  <cwd>/work/project</cwd>\n" +
		"  <shell>zsh</shell>\n" +
		"  <current_date>2026-06-05</current_date>\n" +
		"  <timezone>GMT</timezone>\n" +
		`  <filesystem><workspace_roots><root>/work/project</root></workspace_roots>` +
		`<permission_profile type="disabled"><file_system type="unrestricted" /></permission_profile>` +
		`</filesystem>` + "\n" +
		"</environment_context>"

	got := RenderEnvironmentContext(in)
	if got != want {
		t.Errorf("danger-full-access environment_context mismatch\n want: %q\n got:  %q", want, got)
	}
}

// fsRender renders just the <filesystem> element for comparison.
func fsRender(in EnvironmentContextInput) string {
	return RenderEnvironmentContext(in)
}

// TestFilesystemContextForModeWorkspaceWrite asserts the mode dispatcher now
// returns the workspace-write filesystem context (previously omitted pending a
// capture).
func TestFilesystemContextForModeWorkspaceWrite(t *testing.T) {
	const cwd = "/work/project"
	got := filesystemContextForMode(protocol.SandboxModeWorkspaceWrite, cwd)
	if got == nil {
		t.Fatal("filesystemContextForMode(workspace-write) = nil, want a context")
	}
	wantRender := fsRender(EnvironmentContextInput{Cwd: cwd, Shell: "zsh", Filesystem: WorkspaceWriteFilesystemContext(cwd)})
	gotRender := fsRender(EnvironmentContextInput{Cwd: cwd, Shell: "zsh", Filesystem: got})
	if gotRender != wantRender {
		t.Errorf("workspace-write context mismatch\n want: %q\n got:  %q", wantRender, gotRender)
	}
}

// TestFilesystemContextForModeDangerFullAccess asserts the dispatcher returns
// the disabled/unrestricted context for danger-full-access.
func TestFilesystemContextForModeDangerFullAccess(t *testing.T) {
	got := filesystemContextForMode(protocol.SandboxModeDangerFullAccess, "/work/project")
	if got == nil {
		t.Fatal("filesystemContextForMode(danger-full-access) = nil, want a context")
	}
	if got.ProfileType != "disabled" {
		t.Errorf("danger-full-access profile type = %q, want %q", got.ProfileType, "disabled")
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
