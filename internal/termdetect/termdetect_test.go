package termdetect

import "testing"

// fakeEnvironment is a test double for environment that serves values from a
// map plus injectable tmux/zellij probe results.
type fakeEnvironment struct {
	vars         map[string]string
	tmux         tmuxClientInfo
	zellijFromV  string // explicit injected zellij version (highest priority)
	zellijFromV0 bool   // whether zellijFromV was injected
}

func newFakeEnv() *fakeEnvironment {
	return &fakeEnvironment{vars: map[string]string{}}
}

func (f *fakeEnvironment) withVar(key, value string) *fakeEnvironment {
	f.vars[key] = value
	return f
}

func (f *fakeEnvironment) withTmux(termtype, termname string) *fakeEnvironment {
	f.tmux = tmuxClientInfo{termtype: termtype, termname: termname}
	return f
}

func (f *fakeEnvironment) withZellijVersion(version string) *fakeEnvironment {
	f.zellijFromV = version
	f.zellijFromV0 = true
	return f
}

func (f *fakeEnvironment) lookup(name string) (string, bool) {
	v, ok := f.vars[name]
	return v, ok
}

func (f *fakeEnvironment) tmuxClientInfo() tmuxClientInfo {
	return f.tmux
}

// zellijVersion mirrors the Rust FakeEnvironment: injected version first, then
// the ZELLIJ_VERSION variable fallback.
func (f *fakeEnvironment) zellijVersion() string {
	if f.zellijFromV0 {
		return f.zellijFromV
	}
	return varNonEmpty(f, "ZELLIJ_VERSION")
}

// info is a terse constructor for the expected TerminalInfo in table tests.
func info(name TerminalName, termProgram, version, term string, mux Multiplexer) TerminalInfo {
	return TerminalInfo{
		Name:        name,
		TermProgram: termProgram,
		Version:     version,
		Term:        term,
		Multiplexer: mux,
	}
}

var (
	noMux    = Multiplexer{Kind: MultiplexerNone}
	tmuxMux  = func(v string) Multiplexer { return Multiplexer{Kind: MultiplexerTmux, Version: v} }
	zellMux  = func(v string) Multiplexer { return Multiplexer{Kind: MultiplexerZellij, Version: v} }
	tmuxNone = tmuxMux("")
)

func TestDetectTerminalInfo(t *testing.T) {
	tests := []struct {
		name    string
		env     *fakeEnvironment
		want    TerminalInfo
		wantUA  string
		checkUA bool
	}{
		{
			name: "term_program_with_version",
			env: newFakeEnv().
				withVar("TERM_PROGRAM", "iTerm.app").
				withVar("TERM_PROGRAM_VERSION", "3.5.0").
				withVar("WEZTERM_VERSION", "2024.2"),
			want:    info(TerminalIterm2, "iTerm.app", "3.5.0", "", noMux),
			wantUA:  "iTerm.app/3.5.0",
			checkUA: true,
		},
		{
			name: "term_program_empty_version",
			env: newFakeEnv().
				withVar("TERM_PROGRAM", "iTerm.app").
				withVar("TERM_PROGRAM_VERSION", ""),
			want:    info(TerminalIterm2, "iTerm.app", "", "", noMux),
			wantUA:  "iTerm.app",
			checkUA: true,
		},
		{
			name: "term_program_overrides_wezterm",
			env: newFakeEnv().
				withVar("TERM_PROGRAM", "iTerm.app").
				withVar("WEZTERM_VERSION", "2024.2"),
			want:    info(TerminalIterm2, "iTerm.app", "", "", noMux),
			wantUA:  "iTerm.app",
			checkUA: true,
		},
		{
			name:    "iterm_session_id",
			env:     newFakeEnv().withVar("ITERM_SESSION_ID", "w0t1p0"),
			want:    info(TerminalIterm2, "", "", "", noMux),
			wantUA:  "iTerm.app",
			checkUA: true,
		},
		{
			name:    "apple_term_program",
			env:     newFakeEnv().withVar("TERM_PROGRAM", "Apple_Terminal"),
			want:    info(TerminalAppleTerminal, "Apple_Terminal", "", "", noMux),
			wantUA:  "Apple_Terminal",
			checkUA: true,
		},
		{
			name:    "apple_term_session_id",
			env:     newFakeEnv().withVar("TERM_SESSION_ID", "A1B2C3"),
			want:    info(TerminalAppleTerminal, "", "", "", noMux),
			wantUA:  "Apple_Terminal",
			checkUA: true,
		},
		{
			name:    "ghostty_term_program",
			env:     newFakeEnv().withVar("TERM_PROGRAM", "Ghostty"),
			want:    info(TerminalGhostty, "Ghostty", "", "", noMux),
			wantUA:  "Ghostty",
			checkUA: true,
		},
		{
			name: "vscode_term_program",
			env: newFakeEnv().
				withVar("TERM_PROGRAM", "vscode").
				withVar("TERM_PROGRAM_VERSION", "1.86.0"),
			want:    info(TerminalVsCode, "vscode", "1.86.0", "", noMux),
			wantUA:  "vscode/1.86.0",
			checkUA: true,
		},
		{
			name: "warp_term_program",
			env: newFakeEnv().
				withVar("TERM_PROGRAM", "WarpTerminal").
				withVar("TERM_PROGRAM_VERSION", "v0.2025.12.10.08.12.stable_03"),
			want:    info(TerminalWarpTerminal, "WarpTerminal", "v0.2025.12.10.08.12.stable_03", "", noMux),
			wantUA:  "WarpTerminal/v0.2025.12.10.08.12.stable_03",
			checkUA: true,
		},
		{
			name: "tmux_multiplexer",
			env: newFakeEnv().
				withVar("TMUX", "/tmp/tmux-1000/default,123,0").
				withVar("TERM_PROGRAM", "tmux").
				withTmux("xterm-256color", "screen-256color"),
			want:    info(TerminalUnknown, "xterm-256color", "", "screen-256color", tmuxNone),
			wantUA:  "xterm-256color",
			checkUA: true,
		},
		{
			name: "zellij_multiplexer",
			env:  newFakeEnv().withVar("ZELLIJ", "1"),
			want: info(TerminalUnknown, "", "", "", zellMux("")),
		},
		{
			name: "zellij_multiplexer_version",
			env:  newFakeEnv().withVar("ZELLIJ_VERSION", "0.43.1"),
			want: info(TerminalUnknown, "", "", "", zellMux("0.43.1")),
		},
		{
			name: "zellij_multiplexer_command_version",
			env: newFakeEnv().
				withVar("ZELLIJ", "1").
				withZellijVersion("0.44.1"),
			want: info(TerminalUnknown, "", "", "", zellMux("0.44.1")),
		},
		{
			name: "tmux_client_termtype",
			env: newFakeEnv().
				withVar("TMUX", "/tmp/tmux-1000/default,123,0").
				withVar("TERM_PROGRAM", "tmux").
				withTmux("WezTerm", ""),
			want:    info(TerminalWezTerm, "WezTerm", "", "", tmuxNone),
			wantUA:  "WezTerm",
			checkUA: true,
		},
		{
			name: "tmux_client_termname",
			env: newFakeEnv().
				withVar("TMUX", "/tmp/tmux-1000/default,123,0").
				withVar("TERM_PROGRAM", "tmux").
				withTmux("", "xterm-256color"),
			want:    info(TerminalUnknown, "", "", "xterm-256color", tmuxNone),
			wantUA:  "xterm-256color",
			checkUA: true,
		},
		{
			name: "tmux_term_program_uses_client_termtype",
			env: newFakeEnv().
				withVar("TMUX", "/tmp/tmux-1000/default,123,0").
				withVar("TERM_PROGRAM", "tmux").
				withVar("TERM_PROGRAM_VERSION", "3.6a").
				withTmux("ghostty 1.2.3", "xterm-ghostty"),
			want:    info(TerminalGhostty, "ghostty", "1.2.3", "xterm-ghostty", tmuxMux("3.6a")),
			wantUA:  "ghostty/1.2.3",
			checkUA: true,
		},
		{
			name:    "wezterm_version",
			env:     newFakeEnv().withVar("WEZTERM_VERSION", "2024.2"),
			want:    info(TerminalWezTerm, "", "2024.2", "", noMux),
			wantUA:  "WezTerm/2024.2",
			checkUA: true,
		},
		{
			name: "wezterm_term_program",
			env: newFakeEnv().
				withVar("TERM_PROGRAM", "WezTerm").
				withVar("TERM_PROGRAM_VERSION", "2024.2"),
			want:    info(TerminalWezTerm, "WezTerm", "2024.2", "", noMux),
			wantUA:  "WezTerm/2024.2",
			checkUA: true,
		},
		{
			name:    "wezterm_empty_version",
			env:     newFakeEnv().withVar("WEZTERM_VERSION", ""),
			want:    info(TerminalWezTerm, "", "", "", noMux),
			wantUA:  "WezTerm",
			checkUA: true,
		},
		{
			name:    "wezterm_term",
			env:     newFakeEnv().withVar("TERM", "wezterm"),
			want:    info(TerminalWezTerm, "", "", "wezterm", noMux),
			wantUA:  "wezterm",
			checkUA: true,
		},
		{
			name:    "wezterm_mux_term",
			env:     newFakeEnv().withVar("TERM", "wezterm-mux"),
			want:    info(TerminalWezTerm, "", "", "wezterm-mux", noMux),
			wantUA:  "wezterm-mux",
			checkUA: true,
		},
		{
			name:    "kitty_window_id",
			env:     newFakeEnv().withVar("KITTY_WINDOW_ID", "1"),
			want:    info(TerminalKitty, "", "", "", noMux),
			wantUA:  "kitty",
			checkUA: true,
		},
		{
			name: "kitty_term_program",
			env: newFakeEnv().
				withVar("TERM_PROGRAM", "kitty").
				withVar("TERM_PROGRAM_VERSION", "0.30.1"),
			want:    info(TerminalKitty, "kitty", "0.30.1", "", noMux),
			wantUA:  "kitty/0.30.1",
			checkUA: true,
		},
		{
			name: "kitty_term_over_alacritty",
			env: newFakeEnv().
				withVar("TERM", "xterm-kitty").
				withVar("ALACRITTY_SOCKET", "/tmp/alacritty"),
			want:    info(TerminalKitty, "", "", "", noMux),
			wantUA:  "kitty",
			checkUA: true,
		},
		{
			name:    "alacritty_socket",
			env:     newFakeEnv().withVar("ALACRITTY_SOCKET", "/tmp/alacritty"),
			want:    info(TerminalAlacritty, "", "", "", noMux),
			wantUA:  "Alacritty",
			checkUA: true,
		},
		{
			name: "alacritty_term_program",
			env: newFakeEnv().
				withVar("TERM_PROGRAM", "Alacritty").
				withVar("TERM_PROGRAM_VERSION", "0.13.2"),
			want:    info(TerminalAlacritty, "Alacritty", "0.13.2", "", noMux),
			wantUA:  "Alacritty/0.13.2",
			checkUA: true,
		},
		{
			name:    "alacritty_term",
			env:     newFakeEnv().withVar("TERM", "alacritty"),
			want:    info(TerminalAlacritty, "", "", "", noMux),
			wantUA:  "Alacritty",
			checkUA: true,
		},
		{
			name:    "konsole_version",
			env:     newFakeEnv().withVar("KONSOLE_VERSION", "230800"),
			want:    info(TerminalKonsole, "", "230800", "", noMux),
			wantUA:  "Konsole/230800",
			checkUA: true,
		},
		{
			name: "konsole_term_program",
			env: newFakeEnv().
				withVar("TERM_PROGRAM", "Konsole").
				withVar("TERM_PROGRAM_VERSION", "230800"),
			want:    info(TerminalKonsole, "Konsole", "230800", "", noMux),
			wantUA:  "Konsole/230800",
			checkUA: true,
		},
		{
			name:    "konsole_empty_version",
			env:     newFakeEnv().withVar("KONSOLE_VERSION", ""),
			want:    info(TerminalKonsole, "", "", "", noMux),
			wantUA:  "Konsole",
			checkUA: true,
		},
		{
			name:    "gnome_terminal_screen",
			env:     newFakeEnv().withVar("GNOME_TERMINAL_SCREEN", "1"),
			want:    info(TerminalGnomeTerminal, "", "", "", noMux),
			wantUA:  "gnome-terminal",
			checkUA: true,
		},
		{
			name: "gnome_terminal_term_program",
			env: newFakeEnv().
				withVar("TERM_PROGRAM", "gnome-terminal").
				withVar("TERM_PROGRAM_VERSION", "3.50"),
			want:    info(TerminalGnomeTerminal, "gnome-terminal", "3.50", "", noMux),
			wantUA:  "gnome-terminal/3.50",
			checkUA: true,
		},
		{
			name:    "vte_version",
			env:     newFakeEnv().withVar("VTE_VERSION", "7000"),
			want:    info(TerminalVte, "", "7000", "", noMux),
			wantUA:  "VTE/7000",
			checkUA: true,
		},
		{
			name: "vte_term_program",
			env: newFakeEnv().
				withVar("TERM_PROGRAM", "VTE").
				withVar("TERM_PROGRAM_VERSION", "7000"),
			want:    info(TerminalVte, "VTE", "7000", "", noMux),
			wantUA:  "VTE/7000",
			checkUA: true,
		},
		{
			name:    "vte_empty_version",
			env:     newFakeEnv().withVar("VTE_VERSION", ""),
			want:    info(TerminalVte, "", "", "", noMux),
			wantUA:  "VTE",
			checkUA: true,
		},
		{
			name:    "wt_session",
			env:     newFakeEnv().withVar("WT_SESSION", "1"),
			want:    info(TerminalWindowsTerminal, "", "", "", noMux),
			wantUA:  "WindowsTerminal",
			checkUA: true,
		},
		{
			name: "windows_terminal_term_program",
			env: newFakeEnv().
				withVar("TERM_PROGRAM", "WindowsTerminal").
				withVar("TERM_PROGRAM_VERSION", "1.21"),
			want:    info(TerminalWindowsTerminal, "WindowsTerminal", "1.21", "", noMux),
			wantUA:  "WindowsTerminal/1.21",
			checkUA: true,
		},
		{
			name:    "term_fallback",
			env:     newFakeEnv().withVar("TERM", "xterm-256color"),
			want:    info(TerminalUnknown, "", "", "xterm-256color", noMux),
			wantUA:  "xterm-256color",
			checkUA: true,
		},
		{
			name:    "dumb_term",
			env:     newFakeEnv().withVar("TERM", "dumb"),
			want:    info(TerminalDumb, "", "", "dumb", noMux),
			wantUA:  "dumb",
			checkUA: true,
		},
		{
			name:    "unknown",
			env:     newFakeEnv(),
			want:    info(TerminalUnknown, "", "", "", noMux),
			wantUA:  "unknown",
			checkUA: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectFromEnv(tt.env)
			if got != tt.want {
				t.Errorf("detectFromEnv() = %+v, want %+v", got, tt.want)
			}
			if tt.checkUA {
				if ua := got.userAgentToken(); ua != tt.wantUA {
					t.Errorf("userAgentToken() = %q, want %q", ua, tt.wantUA)
				}
			}
		})
	}
}

func TestIsZellij(t *testing.T) {
	tests := []struct {
		name string
		mux  Multiplexer
		want bool
	}{
		{"zellij", zellMux(""), true},
		{"tmux", tmuxNone, false},
		{"none", noMux, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ti := info(TerminalUnknown, "", "", "", tt.mux)
			if got := ti.IsZellij(); got != tt.want {
				t.Errorf("IsZellij() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMultiplexerPresent(t *testing.T) {
	if noMux.Present() {
		t.Errorf("MultiplexerNone.Present() = true, want false")
	}
	if !tmuxNone.Present() {
		t.Errorf("tmux Present() = false, want true")
	}
	if !zellMux("0.1").Present() {
		t.Errorf("zellij Present() = false, want true")
	}
}

func TestParseZellijVersion(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"prefixed", "zellij 0.44.1", "0.44.1"},
		{"bare", "0.44.1", "0.44.1"},
		{"empty", "", ""},
		{"whitespace_only", "   ", ""},
		{"case_insensitive_prefix", "ZELLIJ 0.45.0", "0.45.0"},
		{"single_token_named_zellij", "zellij", "zellij"},
		{"three_tokens", "zellij 0.44.1 (release)", "0.44.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseZellijVersion(tt.input); got != tt.want {
				t.Errorf("parseZellijVersion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSplitTermProgramAndVersion(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantProgram string
		wantVersion string
	}{
		{"program_and_version", "ghostty 1.2.3", "ghostty", "1.2.3"},
		{"program_only", "WezTerm", "WezTerm", ""},
		{"empty", "", "", ""},
		{"leading_whitespace", "  ghostty   1.2.3 ", "ghostty", "1.2.3"},
		{"three_fields_drops_third", "ghostty 1.2.3 extra", "ghostty", "1.2.3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, v := splitTermProgramAndVersion(tt.input)
			if p != tt.wantProgram || v != tt.wantVersion {
				t.Errorf("splitTermProgramAndVersion(%q) = (%q, %q), want (%q, %q)",
					tt.input, p, v, tt.wantProgram, tt.wantVersion)
			}
		})
	}
}

func TestTerminalNameFromTermProgram(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  TerminalName
	}{
		{"apple", "Apple_Terminal", TerminalAppleTerminal},
		{"ghostty", "Ghostty", TerminalGhostty},
		{"iterm_app", "iTerm.app", TerminalIterm2},
		{"iterm", "iterm", TerminalIterm2},
		{"iterm2", "iTerm2", TerminalIterm2},
		{"warp", "warp", TerminalWarpTerminal},
		{"warp_terminal", "WarpTerminal", TerminalWarpTerminal},
		{"vscode", "vscode", TerminalVsCode},
		{"wezterm", "WezTerm", TerminalWezTerm},
		{"kitty", "kitty", TerminalKitty},
		{"alacritty", "Alacritty", TerminalAlacritty},
		{"konsole", "Konsole", TerminalKonsole},
		{"gnome", "gnome-terminal", TerminalGnomeTerminal},
		{"vte", "VTE", TerminalVte},
		{"windows_terminal", "WindowsTerminal", TerminalWindowsTerminal},
		{"dumb", "dumb", TerminalDumb},
		{"unknown", "something-else", TerminalUnknown},
		// Internal spaces are also stripped (not just trimmed), matching the
		// Rust crate which filters ' '/'-'/'_'/'.' anywhere in the value.
		{"internal_spaces_stripped", "  G H O S T T Y ", TerminalGhostty},
		{"normalized_dots", "i.t.e.r.m", TerminalIterm2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := terminalNameFromTermProgram(tt.input); got != tt.want {
				t.Errorf("terminalNameFromTermProgram(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeHeaderValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"alnum_and_allowed", "iTerm.app/3.5-0_x", "iTerm.app/3.5-0_x"},
		{"spaces_become_underscore", "Apple Terminal", "Apple_Terminal"},
		{"control_and_unicode", "ab\tc\né", "ab_c__"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeHeaderValue(tt.input); got != tt.want {
				t.Errorf("sanitizeHeaderValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNoneIfWhitespace(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"non_empty", "value", "value"},
		{"padded_preserved", "  value  ", "  value  "},
		{"empty", "", ""},
		{"whitespace_only", "  \t\n ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := noneIfWhitespace(tt.input); got != tt.want {
				t.Errorf("noneIfWhitespace(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestPublicDetectDoesNotPanic exercises the exported Detect/UserAgent entry
// points against the real process environment. The result is environment
// dependent, so this only asserts they run without panicking and produce a
// non-empty user-agent token.
func TestPublicDetectDoesNotPanic(t *testing.T) {
	got := Detect()
	_ = got.IsZellij()
	if ua := UserAgent(); ua == "" {
		t.Errorf("UserAgent() returned empty string")
	}
}
