//go:build windows

package sandbox

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsEscapeArg(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"simple", "simple"},
		{"has space", `"has space"`},
		{`quote"inside`, `"quote\"inside"`},
		{`C:\path\to`, `C:\path\to`},
		{`C:\path with space\`, `"C:\path with space\\"`},
		{"", `""`},
	}
	for _, tt := range tests {
		if got := windowsEscapeArg(tt.in); got != tt.want {
			t.Fatalf("windowsEscapeArg(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBuildCommandLine(t *testing.T) {
	got, err := buildCommandLine([]string{"cmd.exe", "/c", "echo hi"})
	if err != nil {
		t.Fatalf("buildCommandLine: %v", err)
	}
	want := `cmd.exe /c "echo hi"`
	if got != want {
		t.Fatalf("buildCommandLine = %q, want %q", got, want)
	}
	if _, err := buildCommandLine(nil); err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestWindowsEnvBlock(t *testing.T) {
	block, err := windowsEnvBlock([]string{"A=1", "B=2"})
	if err != nil {
		t.Fatalf("windowsEnvBlock: %v", err)
	}
	if block == nil {
		t.Fatal("expected non-nil block")
	}

	// Decode the double-NUL-terminated UTF-16 block back into entries.
	entries := decodeEnvBlock(block)
	if len(entries) != 2 || entries[0] != "A=1" || entries[1] != "B=2" {
		t.Fatalf("decoded entries = %v, want [A=1 B=2]", entries)
	}

	// An entry containing a NUL must be rejected.
	if _, err := windowsEnvBlock([]string{"BAD\x00=1"}); err == nil {
		t.Fatal("expected error for env entry containing NUL")
	}

	// An empty environment still yields a valid (terminator-only) block.
	empty, err := windowsEnvBlock(nil)
	if err != nil || empty == nil {
		t.Fatalf("empty env block: block=%v err=%v", empty, err)
	}
}

// decodeEnvBlock reads a double-NUL-terminated UTF-16 environment block into its
// constituent "KEY=VALUE" strings.
func decodeEnvBlock(block *uint16) []string {
	// Read the contiguous UTF-16 units until the double-NUL terminator. A bound is
	// applied so a malformed block cannot loop unboundedly in the test.
	const maxUnits = 1 << 16
	raw := unsafe.Slice(block, maxUnits)

	var entries []string
	start := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] != 0 {
			continue
		}
		if i == start {
			break // empty entry => block terminator
		}
		entries = append(entries, windows.UTF16ToString(raw[start:i]))
		start = i + 1
	}
	return entries
}
