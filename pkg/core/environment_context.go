package core

import (
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/internal/brand"
)

// This file ports codex's environment_context fragment
// (core/src/context/environment_context.rs): the user-role `<environment_context>`
// message carrying cwd/shell/current_date/timezone and the filesystem sandbox
// description. The XML shape, indentation, and escaping match codex byte-for-byte.
//
// Scope: the single-environment case (the common `codex exec` path) plus the
// managed filesystem profile. Multi-environment / network / subagents sections are
// conditional refinements added with the full permission-profile wiring (tracked
// in docs/PARITY.md).

const (
	environmentContextOpenTag  = "<environment_context>"
	environmentContextCloseTag = "</environment_context>"
)

// EnvironmentContextInput carries the rendered values for a single-environment
// `<environment_context>` message.
type EnvironmentContextInput struct {
	// Cwd / Shell describe the single turn environment.
	Cwd   string
	Shell string
	// CurrentDate / Timezone are optional (empty string omits the line).
	CurrentDate string
	Timezone    string
	// Filesystem, when non-nil, renders the <filesystem> section.
	Filesystem *FilesystemContext
}

// FilesystemContext mirrors codex's FileSystemContext: the workspace roots and
// the (managed) permission profile rendered into the <filesystem> element.
type FilesystemContext struct {
	WorkspaceRoots []string
	// ProfileType is "managed", "disabled", or "external".
	ProfileType string
	// Restricted toggles <file_system type="restricted"> vs "unrestricted"
	// (only meaningful for a managed profile).
	Restricted bool
	// Entries are the restricted file-system sandbox entries.
	Entries []FilesystemEntry
	// GlobScanMaxDepth, when non-nil, sets the glob_scan_max_depth attribute.
	GlobScanMaxDepth *int
}

// FilesystemEntry mirrors codex's FileSystemSandboxEntry render: an access mode
// plus exactly one of a special token, a path, or a glob pattern.
type FilesystemEntry struct {
	// Access is "read", "write", or "deny".
	Access string
	// Special / Path / Glob: set exactly one. Special is e.g. ":root".
	Special string
	Path    string
	Glob    string
}

// ReadOnlyFilesystemContext builds the managed/restricted profile codex emits for
// the read-only sandbox: workspace_roots = [cwd] and a single read entry granting
// access to the :root special path.
func ReadOnlyFilesystemContext(cwd string) *FilesystemContext {
	return &FilesystemContext{
		WorkspaceRoots: []string{cwd},
		ProfileType:    "managed",
		Restricted:     true,
		Entries:        []FilesystemEntry{{Access: "read", Special: ":root"}},
	}
}

// WorkspaceWriteFilesystemContext builds the managed/restricted profile codex
// emits for the workspace-write sandbox with no additional writable_roots
// configured (the default). Captured byte-for-byte from codex 0.136.0, it
// mirrors PermissionProfile::workspace_write after
// materialize_project_roots_with_workspace_roots([cwd]):
//
//   - :root read
//   - cwd write (the materialized :workspace_roots / project_roots entry)
//   - :slash_tmp write (exclude_slash_tmp is false by default)
//   - :tmpdir write (exclude_tmpdir_env_var is false by default)
//   - {cwd}/.git, {cwd}/.agents, {cwd}/.codexgo read (the default read-only
//     project-root metadata carveouts, appended unconditionally)
//
// Additional writable_roots (config sandbox_workspace_write.writable_roots) and
// their per-root carveouts are not modeled here — codexgo's headless host does
// not yet thread that config through; tracked in docs/PARITY.md.
func WorkspaceWriteFilesystemContext(cwd string) *FilesystemContext {
	return &FilesystemContext{
		WorkspaceRoots: []string{cwd},
		ProfileType:    "managed",
		Restricted:     true,
		Entries: []FilesystemEntry{
			{Access: "read", Special: ":root"},
			{Access: "write", Path: cwd},
			{Access: "write", Special: ":slash_tmp"},
			{Access: "write", Special: ":tmpdir"},
			{Access: "read", Path: cwd + "/.git"},
			{Access: "read", Path: cwd + "/.agents"},
			{Access: "read", Path: cwd + "/" + brand.DotDirName},
		},
	}
}

// DangerFullAccessFilesystemContext builds the disabled/unrestricted profile
// codex emits for the danger-full-access sandbox: workspace_roots = [cwd] and a
// disabled permission profile with an unrestricted file system. Captured
// byte-for-byte from codex 0.136.0.
func DangerFullAccessFilesystemContext(cwd string) *FilesystemContext {
	return &FilesystemContext{
		WorkspaceRoots: []string{cwd},
		ProfileType:    "disabled",
	}
}

// pushTextElement appends <name>value</name> with codex's XML escaping. Mirrors
// Rust push_text_element + escape_xml.
func pushTextElement(b *strings.Builder, name, value string) {
	b.WriteString("<" + name + ">")
	b.WriteString(escapeXML(value))
	b.WriteString("</" + name + ">")
}

// escapeXML mirrors codex's character-level XML escaping in push_text_element.
func escapeXML(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// render renders the <filesystem> element. Mirrors FileSystemContext::render +
// FileSystemPermissionProfileContext::render + ManagedFileSystemContext::render.
func (f *FilesystemContext) render() string {
	var b strings.Builder
	b.WriteString("<filesystem>")
	if len(f.WorkspaceRoots) > 0 {
		b.WriteString("<workspace_roots>")
		for _, root := range f.WorkspaceRoots {
			pushTextElement(&b, "root", root)
		}
		b.WriteString("</workspace_roots>")
	}
	switch f.ProfileType {
	case "disabled":
		b.WriteString(`<permission_profile type="disabled"><file_system type="unrestricted" /></permission_profile>`)
	case "external":
		b.WriteString(`<permission_profile type="external"><file_system type="external" /></permission_profile>`)
	default: // managed
		b.WriteString(`<permission_profile type="managed">`)
		f.renderManagedFileSystem(&b)
		b.WriteString("</permission_profile>")
	}
	b.WriteString("</filesystem>")
	return b.String()
}

// renderManagedFileSystem renders the <file_system> element for a managed profile.
func (f *FilesystemContext) renderManagedFileSystem(b *strings.Builder) {
	if !f.Restricted {
		b.WriteString(`<file_system type="unrestricted" />`)
		return
	}
	if len(f.Entries) == 0 && f.GlobScanMaxDepth == nil {
		b.WriteString(`<file_system type="restricted" />`)
		return
	}
	b.WriteString(`<file_system type="restricted"`)
	if f.GlobScanMaxDepth != nil {
		b.WriteString(fmt.Sprintf(` glob_scan_max_depth="%d"`, *f.GlobScanMaxDepth))
	}
	b.WriteString(">")
	for _, entry := range f.Entries {
		renderFilesystemEntry(b, entry)
	}
	b.WriteString("</file_system>")
}

// renderFilesystemEntry mirrors render_file_system_entry.
func renderFilesystemEntry(b *strings.Builder, entry FilesystemEntry) {
	b.WriteString(`<entry access="`)
	b.WriteString(entry.Access)
	if entry.Access == "deny" {
		b.WriteString(`" escalatable="false`)
	}
	b.WriteString(`">`)
	switch {
	case entry.Special != "":
		pushTextElement(b, "special", entry.Special)
	case entry.Glob != "":
		pushTextElement(b, "glob", entry.Glob)
	default:
		pushTextElement(b, "path", entry.Path)
	}
	b.WriteString("</entry>")
}

// RenderEnvironmentContext builds the full `<environment_context>` user message,
// mirroring EnvironmentContext::body + the fragment marker wrapping
// (open_tag + body + close_tag, body = "\n" + lines joined by "\n" + "\n").
func RenderEnvironmentContext(in EnvironmentContextInput) string {
	var lines []string
	lines = append(lines, "  <cwd>"+in.Cwd+"</cwd>")
	lines = append(lines, "  <shell>"+in.Shell+"</shell>")
	if in.CurrentDate != "" {
		lines = append(lines, "  <current_date>"+in.CurrentDate+"</current_date>")
	}
	if in.Timezone != "" {
		lines = append(lines, "  <timezone>"+in.Timezone+"</timezone>")
	}
	if in.Filesystem != nil {
		lines = append(lines, "  "+in.Filesystem.render())
	}
	body := "\n" + strings.Join(lines, "\n") + "\n"
	return environmentContextOpenTag + body + environmentContextCloseTag
}
