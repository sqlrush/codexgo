package cli

import (
	"strings"
	"testing"
)

func TestCompletionScript(t *testing.T) {
	tests := []struct {
		shell    string
		wantOK   bool
		mustHave string
	}{
		{shell: "bash", wantOK: true, mustHave: "complete -F _codex codex"},
		{shell: "zsh", wantOK: true, mustHave: "#compdef codex"},
		{shell: "fish", wantOK: true, mustHave: "complete -c codex"},
		{shell: "powershell", wantOK: true, mustHave: "Register-ArgumentCompleter"},
		{shell: "elvish", wantOK: true, mustHave: "arg-completer"},
		{shell: "tcsh", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			script, ok := completionScript(tt.shell)
			if ok != tt.wantOK {
				t.Fatalf("completionScript(%q) ok = %v, want %v", tt.shell, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if !strings.Contains(script, tt.mustHave) {
				t.Errorf("script for %q missing %q:\n%s", tt.shell, tt.mustHave, script)
			}
			if !strings.Contains(script, "exec") {
				t.Errorf("script for %q missing subcommand names", tt.shell)
			}
		})
	}
}
