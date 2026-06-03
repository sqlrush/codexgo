package cli

import (
	"reflect"
	"testing"
)

func TestParseCommandLine(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantSub     string
		wantSubArgs []string
		wantProfile string
		wantRemote  string
		wantStrict  bool
		wantHelp    bool
		wantVersion bool
		wantErr     bool
	}{
		{
			name: "no args",
		},
		{
			name:        "exec subcommand with prompt",
			args:        []string{"exec", "hello"},
			wantSub:     "exec",
			wantSubArgs: []string{"hello"},
		},
		{
			name:        "exec alias e",
			args:        []string{"e", "hi"},
			wantSub:     "exec",
			wantSubArgs: []string{"hi"},
		},
		{
			name:        "apply alias a",
			args:        []string{"a", "patch.diff"},
			wantSub:     "apply",
			wantSubArgs: []string{"patch.diff"},
		},
		{
			name:        "root config override before subcommand",
			args:        []string{"-c", "model=gpt-5", "exec", "go"},
			wantSub:     "exec",
			wantSubArgs: []string{"go"},
		},
		{
			name:        "profile flag",
			args:        []string{"-p", "work", "exec"},
			wantSub:     "exec",
			wantProfile: "work",
		},
		{
			name:        "profile equals form",
			args:        []string{"--profile=work", "doctor"},
			wantSub:     "doctor",
			wantProfile: "work",
		},
		{
			name:       "remote flag captured",
			args:       []string{"--remote", "ws://h:1", "exec"},
			wantSub:    "exec",
			wantRemote: "ws://h:1",
		},
		{
			name:       "strict config",
			args:       []string{"--strict-config", "doctor"},
			wantSub:    "doctor",
			wantStrict: true,
		},
		{
			name:     "version flag",
			args:     []string{"--version"},
			wantHelp: false,
			// version short-circuits in Run; here we just record it parsed.
			wantVersion: true,
		},
		{
			name:     "help flag",
			args:     []string{"--help"},
			wantHelp: true,
		},
		{
			name:        "unknown leading flag forwards to default",
			args:        []string{"--some-tui-flag", "value"},
			wantSub:     "",
			wantSubArgs: []string{"--some-tui-flag", "value"},
		},
		{
			name:        "non-subcommand positional forwards to default",
			args:        []string{"just a prompt"},
			wantSub:     "",
			wantSubArgs: []string{"just a prompt"},
		},
		{
			name:        "double dash terminates root flags",
			args:        []string{"--", "exec", "x"},
			wantSub:     "",
			wantSubArgs: []string{"exec", "x"},
		},
		{
			name:    "missing flag value",
			args:    []string{"-c"},
			wantErr: true,
		},
		{
			name:    "unknown feature toggle",
			args:    []string{"--enable", "totally_unknown"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCommandLine(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Subcommand != tt.wantSub {
				t.Errorf("subcommand = %q, want %q", got.Subcommand, tt.wantSub)
			}
			if tt.wantSubArgs != nil && !reflect.DeepEqual(got.SubcommandArgs, tt.wantSubArgs) {
				t.Errorf("subcommand args = %v, want %v", got.SubcommandArgs, tt.wantSubArgs)
			}
			if got.Root.Profile != tt.wantProfile {
				t.Errorf("profile = %q, want %q", got.Root.Profile, tt.wantProfile)
			}
			if got.Root.Remote != tt.wantRemote {
				t.Errorf("remote = %q, want %q", got.Root.Remote, tt.wantRemote)
			}
			if got.Root.StrictConfig != tt.wantStrict {
				t.Errorf("strict = %v, want %v", got.Root.StrictConfig, tt.wantStrict)
			}
			if got.Root.ShowHelp != tt.wantHelp {
				t.Errorf("help = %v, want %v", got.Root.ShowHelp, tt.wantHelp)
			}
			if got.Root.ShowVersion != tt.wantVersion {
				t.Errorf("version = %v, want %v", got.Root.ShowVersion, tt.wantVersion)
			}
		})
	}
}

func TestFeatureToggleOverrides(t *testing.T) {
	got, err := ParseCommandLine([]string{"--enable", "shell_tool", "--disable", "unified_exec", "exec"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw := got.Root.Overrides.Raw()
	want := []string{"features.shell_tool=true", "features.unified_exec=false"}
	if !reflect.DeepEqual(raw, want) {
		t.Errorf("overrides = %v, want %v", raw, want)
	}
}
