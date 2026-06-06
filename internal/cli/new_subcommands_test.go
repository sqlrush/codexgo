package cli

import (
	"context"
	"strings"
	"testing"
)

// TestNewSubcommandHelp verifies each newly added subcommand prints help and
// exits 0 for -h / --help, via the help printer registry and the subcommand's
// own --help path.
func TestNewSubcommandHelp(t *testing.T) {
	tests := []struct {
		name     string
		argv     []string
		mustHave string
	}{
		{"cloud", []string{"codex", "cloud", "--help"}, "Codex Cloud"},
		{"plugin", []string{"codex", "plugin", "--help"}, "Manage Codex plugins"},
		{"review", []string{"codex", "review", "--help"}, "code review"},
		{"exec-server", []string{"codex", "exec-server", "--help"}, "exec-server"},
		{"update", []string{"codex", "update", "--help"}, "Update Codex"},
		{"app", []string{"codex", "app", "--help"}, "desktop app"},
		{"remote-control", []string{"codex", "remote-control", "--help"}, "remote control"},
		{"help", []string{"codex", "help"}, "Usage: codex"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out, _ := runWith(t, tt.argv, "")
			if code != 0 {
				t.Errorf("code = %d, want 0", code)
			}
			if !strings.Contains(out, tt.mustHave) {
				t.Errorf("stdout = %q, want substring %q", out, tt.mustHave)
			}
		})
	}
}

// TestHelpForSubcommand verifies `codex help <cmd>` prints that subcommand's help.
func TestHelpForSubcommand(t *testing.T) {
	tests := []struct {
		cmd      string
		mustHave string
	}{
		{"exec", "Run Codex non-interactively"},
		{"cloud", "Codex Cloud"},
		{"plugin", "Manage Codex plugins"},
		{"remote-control", "remote control"},
		{"app", "desktop app"},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			code, out, _ := runWith(t, []string{"codex", "help", tt.cmd}, "")
			if code != 0 {
				t.Errorf("code = %d, want 0", code)
			}
			if !strings.Contains(out, tt.mustHave) {
				t.Errorf("help %s stdout = %q, want substring %q", tt.cmd, out, tt.mustHave)
			}
		})
	}
}

// TestHelpUnknownCommand verifies `codex help <unknown>` reports an error.
func TestHelpUnknownCommand(t *testing.T) {
	code, _, errOut := runWith(t, []string{"codex", "help", "not-a-command"}, "")
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "unknown command") {
		t.Errorf("stderr = %q", errOut)
	}
}

// TestUpdateNotice verifies `codex update` emits a clear notice and exits non-zero.
func TestUpdateNotice(t *testing.T) {
	code, _, errOut := runWith(t, []string{"codex", "update"}, "")
	if code == 0 {
		t.Errorf("code = %d, want non-zero", code)
	}
	if !strings.Contains(errOut, "self-update is not available") {
		t.Errorf("stderr = %q", errOut)
	}
}

// TestRemoteControlNotice verifies `codex remote-control start` emits a clear
// notice and exits non-zero (no daemon lifecycle wired).
func TestRemoteControlNotice(t *testing.T) {
	code, _, errOut := runWith(t, []string{"codex", "remote-control", "start"}, "")
	if code == 0 {
		t.Errorf("code = %d, want non-zero", code)
	}
	if !strings.Contains(errOut, "daemon lifecycle") {
		t.Errorf("stderr = %q", errOut)
	}
}

// TestCloudNoActionNotice verifies `codex cloud` with no action emits a notice
// and exits non-zero (interactive browser not ported) using the offline mock
// backend to avoid auth.
func TestCloudNoActionNotice(t *testing.T) {
	t.Setenv("CODEXGO_CLOUD_TASKS_MODE", "mock")
	code, _, errOut := runWith(t, []string{"codex", "cloud"}, "")
	if code == 0 {
		t.Errorf("code = %d, want non-zero", code)
	}
	if !strings.Contains(errOut, "interactive task browser") {
		t.Errorf("stderr = %q", errOut)
	}
}

// TestCloudListMock verifies `codex cloud list` works against the offline mock
// backend (list works offline / without auth).
func TestCloudListMock(t *testing.T) {
	t.Setenv("CODEXGO_CLOUD_TASKS_MODE", "mock")
	code, out, errOut := runWith(t, []string{"codex", "cloud", "list"}, "")
	if code != 0 {
		t.Errorf("code = %d, want 0 (stderr=%q)", code, errOut)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("expected non-empty mock task list, got empty stdout")
	}
}

// TestPluginListOffline verifies `codex plugin list` works offline (exit 0)
// even when no marketplaces are configured.
func TestPluginListOffline(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEXGO_HOME", home)
	code, _, errOut := runWith(t, []string{"codex", "plugin", "list"}, "")
	if code != 0 {
		t.Errorf("code = %d, want 0 (stderr=%q)", code, errOut)
	}
}

// TestExecServerStdioInitialize verifies the exec-server stdio loop answers an
// initialize request with a result envelope.
func TestExecServerStdioInitialize(t *testing.T) {
	in := strings.NewReader(`{"id":1,"method":"initialize","params":{"clientName":"test","resumeSessionId":null}}` + "\n")
	var out strings.Builder
	err := serveExecServerStdio(context.Background(), in, &out)
	if err != nil {
		t.Fatalf("serveExecServerStdio error: %v", err)
	}
	if !strings.Contains(out.String(), `"result"`) || !strings.Contains(out.String(), `"sessionId"`) {
		t.Errorf("response = %q, want a result with sessionId", out.String())
	}
}

// TestExecServerUnsupportedTransport verifies a ws:// listener is rejected with a
// clear notice and non-zero exit.
func TestExecServerUnsupportedTransport(t *testing.T) {
	code, _, errOut := runWith(t, []string{"codex", "exec-server", "--listen", "ws://127.0.0.1:0"}, "")
	if code == 0 {
		t.Errorf("code = %d, want non-zero", code)
	}
	if !strings.Contains(errOut, "not supported") {
		t.Errorf("stderr = %q", errOut)
	}
}

// TestCompletionEnumeratesAllSubcommands verifies each shell completion script
// lists every registered subcommand and the documented top-level flags.
func TestCompletionEnumeratesAllSubcommands(t *testing.T) {
	for _, shell := range completionShells {
		t.Run(shell, func(t *testing.T) {
			script, ok := completionScript(shell)
			if !ok {
				t.Fatalf("completionScript(%q) not ok", shell)
			}
			for name := range handlers {
				if !strings.Contains(script, name) {
					t.Errorf("%s completion missing subcommand %q", shell, name)
				}
			}
			// The documented top-level long flags must appear in every script.
			// Shells render long flags differently (bash/zsh/powershell/elvish
			// use `--config`; fish uses `-l config`), so assert on the bare long
			// name, which is present regardless of the per-shell decoration.
			for _, flag := range []string{
				"config", "enable", "disable", "remote",
				"remote-auth-token-env", "strict-config", "help", "version",
			} {
				if !strings.Contains(script, flag) {
					t.Errorf("%s completion missing flag %q", shell, flag)
				}
			}
		})
	}
}
