package pluginutil

import "testing"

func TestIsConnectorIDAllowedForOriginator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		connectorID string
		originator  string
		want        bool
	}{
		{
			name:        "general disallowed for default originator",
			connectorID: "asdk_app_6938a94a61d881918ef32cb999ff937c",
			originator:  "codex_cli_rs",
			want:        false,
		},
		{
			name:        "another general disallowed id",
			connectorID: "connector_69272cb413a081919685ec3c88d1744e",
			originator:  "codex_cli_rs",
			want:        false,
		},
		{
			name:        "unknown id allowed for default originator",
			connectorID: "connector_totally_fine",
			originator:  "codex_cli_rs",
			want:        true,
		},
		{
			name:        "first-party-chat blocked id is blocked for atlas",
			connectorID: "connector_0f9c9d4592e54d0a9a12b3f44a1e2010",
			originator:  "codex_atlas",
			want:        false,
		},
		{
			name:        "first-party-chat blocked id is blocked for chatgpt desktop",
			connectorID: "connector_0f9c9d4592e54d0a9a12b3f44a1e2010",
			originator:  "codex_chatgpt_desktop",
			want:        false,
		},
		{
			name:        "general blocked id allowed for first-party-chat",
			connectorID: "asdk_app_6938a94a61d881918ef32cb999ff937c",
			originator:  "codex_atlas",
			want:        true,
		},
		{
			name:        "first-party-chat blocked id allowed for default originator",
			connectorID: "connector_0f9c9d4592e54d0a9a12b3f44a1e2010",
			originator:  "codex_cli_rs",
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isConnectorIDAllowedForOriginator(tt.connectorID, tt.originator); got != tt.want {
				t.Fatalf("isConnectorIDAllowedForOriginator(%q, %q) = %v, want %v",
					tt.connectorID, tt.originator, got, tt.want)
			}
		})
	}
}

func TestIsConnectorIDAllowedUsesEnvOriginator(t *testing.T) {
	// Not parallel: mutates the process environment.
	t.Setenv(originatorOverrideEnvVar, "codex_atlas")

	// Under a first-party-chat originator, the general blocked id is allowed but
	// the first-party-chat blocked id is not.
	if !IsConnectorIDAllowed("asdk_app_6938a94a61d881918ef32cb999ff937c") {
		t.Fatalf("expected general blocked id to be allowed for codex_atlas")
	}
	if IsConnectorIDAllowed("connector_0f9c9d4592e54d0a9a12b3f44a1e2010") {
		t.Fatalf("expected first-party-chat blocked id to be blocked for codex_atlas")
	}
}

func TestSanitizeName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "lowercases ascii letters", input: "MyApp", want: "myapp"},
		{name: "spaces become underscores", input: "My App", want: "my_app"},
		{name: "punctuation collapses to single separators", input: "a!!b", want: "a__b"},
		{name: "leading and trailing junk trimmed", input: "--Hello--", want: "hello"},
		{name: "digits preserved", input: "tool42", want: "tool42"},
		{name: "non-ascii becomes separators", input: "café", want: "caf"},
		{name: "all separators becomes app", input: "***", want: "app"},
		{name: "empty becomes app", input: "", want: "app"},
		{name: "underscore is not alphanumeric so becomes separator", input: "a_b", want: "a_b"},
		{name: "mixed punctuation and hyphens", input: "foo-bar baz", want: "foo_bar_baz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := SanitizeName(tt.input); got != tt.want {
				t.Fatalf("SanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "hyphenates separators", input: "My App", want: "my-app"},
		{name: "trims edge hyphens", input: " hello ", want: "hello"},
		{name: "empty becomes app", input: "   ", want: "app"},
		{name: "alnum preserved lowercased", input: "ABC123", want: "abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeSlug(tt.input); got != tt.want {
				t.Fatalf("sanitizeSlug(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsFirstPartyChatOriginator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  bool
	}{
		{input: "codex_atlas", want: true},
		{input: "codex_chatgpt_desktop", want: true},
		{input: "codex_cli_rs", want: false},
		{input: "", want: false},
		{input: "codex_atlas ", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			if got := IsFirstPartyChatOriginator(tt.input); got != tt.want {
				t.Fatalf("IsFirstPartyChatOriginator(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
