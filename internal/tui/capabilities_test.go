package tui

import "testing"

// fakeEnv builds an environ from a map.
func fakeEnv(m map[string]string) environ {
	return func(key string) string { return m[key] }
}

func TestClassifyColorLevel(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want StdoutColorLevel
	}{
		{"colorterm truecolor", map[string]string{"COLORTERM": "truecolor"}, ColorLevelTrueColor},
		{"colorterm 24bit", map[string]string{"COLORTERM": "24bit"}, ColorLevelTrueColor},
		{"term 256color", map[string]string{"TERM": "xterm-256color"}, ColorLevelAnsi256},
		{"term direct", map[string]string{"TERM": "xterm-direct"}, ColorLevelTrueColor},
		{"plain xterm", map[string]string{"TERM": "xterm"}, ColorLevelAnsi16},
		{"dumb term", map[string]string{"TERM": "dumb"}, ColorLevelUnknown},
		{"empty", map[string]string{}, ColorLevelUnknown},
		{"force color 0", map[string]string{"FORCE_COLOR": "0", "COLORTERM": "truecolor"}, ColorLevelUnknown},
		{"force color 3", map[string]string{"FORCE_COLOR": "3"}, ColorLevelTrueColor},
		{"force color 2", map[string]string{"FORCE_COLOR": "2"}, ColorLevelAnsi256},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyColorLevel(fakeEnv(tt.env)); got != tt.want {
				t.Fatalf("classifyColorLevel(%v) = %v, want %v", tt.env, got, tt.want)
			}
		})
	}
}

func TestDetectCapabilitiesNoColor(t *testing.T) {
	caps := detectCapabilities(fakeEnv(map[string]string{
		"NO_COLOR":  "1",
		"COLORTERM": "truecolor",
	}))
	if !caps.NoColor {
		t.Fatal("expected NoColor true")
	}
	if caps.ColorLevel != ColorLevelUnknown {
		t.Fatalf("NO_COLOR should force ColorLevelUnknown, got %v", caps.ColorLevel)
	}
	if caps.SupportsColor() {
		t.Fatal("SupportsColor should be false under NO_COLOR")
	}
}

func TestDetectCapabilitiesMultiplexers(t *testing.T) {
	zellij := detectCapabilities(fakeEnv(map[string]string{"ZELLIJ": "0", "TERM": "xterm-256color"}))
	if !zellij.IsZellij {
		t.Fatal("expected IsZellij true")
	}
	tmux := detectCapabilities(fakeEnv(map[string]string{"TMUX": "/tmp/tmux", "TERM": "screen-256color"}))
	if !tmux.IsTmux {
		t.Fatal("expected IsTmux true")
	}
	vscode := detectCapabilities(fakeEnv(map[string]string{"TERM_PROGRAM": "vscode", "TERM": "xterm-256color"}))
	if !vscode.IsVSCode {
		t.Fatal("expected IsVSCode true")
	}
}

func TestCapabilitiesStringWidth(t *testing.T) {
	caps := Capabilities{ColorLevel: ColorLevelTrueColor}
	if got := caps.StringWidth("hello"); got != 5 {
		t.Fatalf("StringWidth(hello) = %d, want 5", got)
	}
	// A wide CJK rune counts as two cells.
	if got := caps.StringWidth("中"); got != 2 {
		t.Fatalf("StringWidth(CJK) = %d, want 2", got)
	}
}
