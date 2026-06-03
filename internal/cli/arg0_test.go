package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDispatchArg0Routing(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantHandled bool
	}{
		{name: "no args", args: nil, wantHandled: false},
		{name: "plain codex", args: []string{"codex", "exec"}, wantHandled: false},
		{name: "apply_patch alias", args: []string{"/usr/bin/apply_patch"}, wantHandled: true},
		{name: "applypatch typo alias", args: []string{"applypatch"}, wantHandled: true},
		{name: "fs helper marker", args: []string{"codex", fsHelperArg1}, wantHandled: true},
		{name: "core apply patch marker", args: []string{"codex", coreApplyPatchArg1, "*** Begin Patch\n*** End Patch"}, wantHandled: true},
		{name: "regular subcommand not handled", args: []string{"codex", "doctor"}, wantHandled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			_, handled := DispatchArg0(tt.args, Arg0Streams{
				Stdin:  strings.NewReader(""),
				Stdout: &out,
				Stderr: &errOut,
			})
			if handled != tt.wantHandled {
				t.Errorf("handled = %v, want %v", handled, tt.wantHandled)
			}
		})
	}
}

func TestDispatchArg0ApplyPatchCreatesFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	var out, errOut bytes.Buffer
	patch := "*** Begin Patch\n*** Add File: created.txt\n+content here\n*** End Patch"
	code, handled := DispatchArg0([]string{"apply_patch", patch}, Arg0Streams{
		Stdin:  strings.NewReader(""),
		Stdout: &out,
		Stderr: &errOut,
	})
	if !handled {
		t.Fatalf("expected handled=true")
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errOut.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, "created.txt"))
	if err != nil {
		t.Fatalf("reading created file: %v", err)
	}
	if string(got) != "content here\n" {
		t.Errorf("file content = %q", string(got))
	}
}

func TestDispatchArg0ApplyPatchUsageError(t *testing.T) {
	var out, errOut bytes.Buffer
	code, handled := DispatchArg0([]string{"apply_patch"}, Arg0Streams{
		Stdin:  strings.NewReader(""),
		Stdout: &out,
		Stderr: &errOut,
	})
	if !handled || code != 1 {
		t.Fatalf("handled=%v code=%d, want handled=true code=1", handled, code)
	}
	if !strings.Contains(errOut.String(), "usage: apply_patch") {
		t.Errorf("stderr = %q", errOut.String())
	}
}
