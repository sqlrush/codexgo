package shellcmd

import "testing"

func TestLooksLikeURL(t *testing.T) {
	tests := []struct {
		token string
		want  bool
	}{
		{"https://example.com", true},
		{"http://example.com/path", true},
		{"'https://example.com'", true},
		{"(https://example.com)", true},
		{"https://example.com;", true},
		{"Start-Process('https://example.com')", true},
		{`"https://example.com"`, true},
		{"ftp://example.com", false},
		{"file:///etc/passwd", false},
		{"example.com", false},
		{"not-a-url", false},
		{"", false},
		{"-n", false},
	}
	for _, tc := range tests {
		t.Run(tc.token, func(t *testing.T) {
			if got := LooksLikeURL(tc.token); got != tc.want {
				t.Errorf("LooksLikeURL(%q) = %v, want %v", tc.token, got, tc.want)
			}
		})
	}
}

func TestArgsHaveURL(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"contains_url", []string{"curl", "-sSL", "https://example.com"}, true},
		{"no_url", []string{"ls", "-la", "src"}, false},
		{"empty", nil, false},
		{"url_with_punct", []string{"start", "(https://x.io);"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ArgsHaveURL(tc.args); got != tc.want {
				t.Errorf("ArgsHaveURL(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}
