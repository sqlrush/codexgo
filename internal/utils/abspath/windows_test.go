package abspath

import "testing"

// withWindows runs fn with the package treating paths as Windows paths, then
// restores the previous setting. This lets the Windows path grammar be tested on
// any host, mirroring the reference crate's `normalize_for_native_workdir_with_
// flag` test seam.
func withWindows(t *testing.T, fn func()) {
	t.Helper()
	prev := isWindows
	isWindows = func() bool { return true }
	defer func() { isWindows = prev }()
	fn()
}

func TestWindowsResolvePathAgainstBase(t *testing.T) {
	withWindows(t, func() {
		tests := []struct {
			name string
			path string
			base string
			want string
		}{
			{"root-relative uses base prefix", `\path\to\file`, `C:\base\cwd`, `C:\path\to\file`},
			{"drive-relative uses path prefix and base tail", `D:path\to\file`, `C:\base\cwd`, `D:\base\cwd\path\to\file`},
			{"absolute ignores base", `C:\abs\file`, `D:\base`, `C:\abs\file`},
			{"relative joined to base", `sub\file`, `C:\base`, `C:\base\sub\file`},
			{"dots normalized", `sub\..\file`, `C:\base`, `C:\base\file`},
			{"forward slashes accepted", `sub/file`, `C:\base`, `C:\base\sub\file`},
		}
		for _, tc := range tests {
			got := ResolvePathAgainstBase(tc.path, tc.base)
			if got.Path() != tc.want {
				t.Errorf("%s: ResolvePathAgainstBase(%q, %q) = %q, want %q", tc.name, tc.path, tc.base, got.Path(), tc.want)
			}
		}
	})
}

func TestWindowsIsAbsolute(t *testing.T) {
	withWindows(t, func() {
		tests := []struct {
			in   string
			want bool
		}{
			{`C:\x`, true},
			{`C:x`, false},     // drive-relative
			{`\x`, false},      // rooted but prefix-less
			{`\\s\sh\x`, true}, // UNC
			{`rel\x`, false},
		}
		for _, tc := range tests {
			if got := isAbsolute(tc.in); got != tc.want {
				t.Errorf("isAbsolute(%q) = %v, want %v", tc.in, got, tc.want)
			}
		}
	})
}

func TestWindowsHasRoot(t *testing.T) {
	withWindows(t, func() {
		tests := []struct {
			in   string
			want bool
		}{
			{`\x`, true},
			{`C:\x`, true},
			{`C:x`, false},
			{`rel\x`, false},
			{`\\s\sh\x`, true},
		}
		for _, tc := range tests {
			if got := hasRoot(tc.in); got != tc.want {
				t.Errorf("hasRoot(%q) = %v, want %v", tc.in, got, tc.want)
			}
		}
	})
}

func TestWindowsUNCParsing(t *testing.T) {
	withWindows(t, func() {
		got := normalizePath(`\\server\share\a\..\b`)
		want := `\\server\share\b`
		if got != want {
			t.Errorf("normalizePath UNC = %q, want %q", got, want)
		}
	})
}

func TestWindowsDriveOnlyPrefixGetsRoot(t *testing.T) {
	withWindows(t, func() {
		// Drive-relative path whose only component is the prefix resolves to the
		// drive root, mirroring the Rust `path_with_base` early return.
		got := ResolvePathAgainstBase(`D:`, `C:\base\cwd`)
		if want := `D:\`; got.Path() != want {
			t.Errorf("ResolvePathAgainstBase(D:, ...) = %q, want %q", got.Path(), want)
		}
	})
}

func TestWindowsVerbatimPrefixNormalizedInConstructor(t *testing.T) {
	withWindows(t, func() {
		got, err := FromAbsolutePathChecked(`\\?\D:\c\x\worktrees\2508\swift-base`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := `D:\c\x\worktrees\2508\swift-base`; got.Path() != want {
			t.Errorf("got %q, want %q", got.Path(), want)
		}
	})
}

func TestWindowsParentAndAncestors(t *testing.T) {
	withWindows(t, func() {
		buf, err := FromAbsolutePathChecked(`C:\a\b\c`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		anc := buf.Ancestors()
		want := []string{`C:\a\b\c`, `C:\a\b`, `C:\a`, `C:\`}
		if len(anc) != len(want) {
			t.Fatalf("got %d ancestors, want %d", len(anc), len(want))
		}
		for i := range want {
			if anc[i].Path() != want[i] {
				t.Errorf("ancestor[%d] = %q, want %q", i, anc[i].Path(), want[i])
			}
		}
		if _, ok := anc[len(anc)-1].Parent(); ok {
			t.Errorf("drive root should have no parent")
		}
	})
}
