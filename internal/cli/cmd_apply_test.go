package cli

import "testing"

func TestParseApplyArgs(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantPath      string
		wantRevert    bool
		wantPreflight bool
		wantHelp      bool
		wantErr       bool
	}{
		{name: "no args (stdin)", args: nil},
		{name: "patch file", args: []string{"my.diff"}, wantPath: "my.diff"},
		{name: "revert short", args: []string{"-R", "my.diff"}, wantRevert: true, wantPath: "my.diff"},
		{name: "revert long", args: []string{"--revert"}, wantRevert: true},
		{name: "check", args: []string{"--check", "my.diff"}, wantPreflight: true, wantPath: "my.diff"},
		{name: "help", args: []string{"--help"}, wantHelp: true},
		{name: "stdin dash", args: []string{"-"}, wantPath: "-"},
		{name: "unknown flag", args: []string{"--nope"}, wantErr: true},
		{name: "two positionals", args: []string{"a", "b"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseApplyArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.patchPath != tt.wantPath {
				t.Errorf("patchPath = %q, want %q", got.patchPath, tt.wantPath)
			}
			if got.revert != tt.wantRevert {
				t.Errorf("revert = %v, want %v", got.revert, tt.wantRevert)
			}
			if got.preflight != tt.wantPreflight {
				t.Errorf("preflight = %v, want %v", got.preflight, tt.wantPreflight)
			}
			if got.help != tt.wantHelp {
				t.Errorf("help = %v, want %v", got.help, tt.wantHelp)
			}
		})
	}
}
