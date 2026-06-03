package gitutils

import (
	"reflect"
	"testing"
)

func TestParseGitApplyOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		stdout         string
		stderr         string
		wantApplied    []string
		wantSkipped    []string
		wantConflicted []string
	}{
		{
			name:           "unescapes quoted paths",
			stderr:         "error: patch failed: \"hello\\tworld.txt\":1\n",
			wantApplied:    []string{},
			wantSkipped:    []string{"hello\tworld.txt"},
			wantConflicted: []string{},
		},
		{
			name:           "applied cleanly",
			stdout:         "Checking patch ok.txt...\nApplied patch ok.txt cleanly.\n",
			wantApplied:    []string{"ok.txt"},
			wantSkipped:    []string{},
			wantConflicted: []string{},
		},
		{
			name:           "applied with conflicts",
			stdout:         "Applied patch foo.txt with conflicts.\n",
			wantApplied:    []string{},
			wantSkipped:    []string{},
			wantConflicted: []string{"foo.txt"},
		},
		{
			name:           "does not apply marks skipped",
			stderr:         "error: file.txt: patch does not apply\n",
			wantApplied:    []string{},
			wantSkipped:    []string{"file.txt"},
			wantConflicted: []string{},
		},
		{
			name:           "does not exist in index marks skipped",
			stderr:         "error: ghost.txt: does not exist in index\n",
			wantApplied:    []string{},
			wantSkipped:    []string{"ghost.txt"},
			wantConflicted: []string{},
		},
		{
			name:           "unmerged line marks conflicted",
			stdout:         "U merged.txt\n",
			wantApplied:    []string{},
			wantSkipped:    []string{},
			wantConflicted: []string{"merged.txt"},
		},
		{
			name:           "conflict precedence over applied",
			stdout:         "Applied patch foo.txt cleanly.\nApplied patch foo.txt with conflicts.\n",
			wantApplied:    []string{},
			wantSkipped:    []string{},
			wantConflicted: []string{"foo.txt"},
		},
		{
			name:           "three way failed attributes to last seen",
			stdout:         "Checking patch lonely.txt...\nFailed to perform three-way merge...\n",
			wantApplied:    []string{},
			wantSkipped:    []string{"lonely.txt"},
			wantConflicted: []string{},
		},
		{
			name:           "empty output",
			wantApplied:    []string{},
			wantSkipped:    []string{},
			wantConflicted: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			applied, skipped, conflicted := ParseGitApplyOutput(tc.stdout, tc.stderr)
			if !reflect.DeepEqual(applied, tc.wantApplied) {
				t.Errorf("applied = %#v, want %#v", applied, tc.wantApplied)
			}
			if !reflect.DeepEqual(skipped, tc.wantSkipped) {
				t.Errorf("skipped = %#v, want %#v", skipped, tc.wantSkipped)
			}
			if !reflect.DeepEqual(conflicted, tc.wantConflicted) {
				t.Errorf("conflicted = %#v, want %#v", conflicted, tc.wantConflicted)
			}
		})
	}
}

func TestQuoteShell(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"simple", "simple"},
		{"a/b-c.d:e@f%g+h", "a/b-c.d:e@f%g+h"},
		{"has space", "'has space'"},
		{"it's", `'it'\''s'`},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := quoteShell(tc.in); got != tc.want {
				t.Fatalf("quoteShell(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
