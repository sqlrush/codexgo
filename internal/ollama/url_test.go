package ollama

import "testing"

func TestIsOpenAICompatibleBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    bool
	}{
		{"v1 suffix", "http://localhost:11434/v1", true},
		{"v1 suffix trailing slash", "http://localhost:11434/v1/", true},
		{"native root", "http://localhost:11434", false},
		{"native root trailing slash", "http://localhost:11434/", false},
		{"v1 elsewhere", "http://localhost:11434/v1/models", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsOpenAICompatibleBaseURL(tc.baseURL); got != tc.want {
				t.Errorf("IsOpenAICompatibleBaseURL(%q) = %v, want %v", tc.baseURL, got, tc.want)
			}
		})
	}
}

func TestBaseURLToHostRoot(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{"v1 suffix", "http://localhost:11434/v1", "http://localhost:11434"},
		{"native root", "http://localhost:11434", "http://localhost:11434"},
		{"native trailing slash", "http://localhost:11434/", "http://localhost:11434"},
		{"v1 trailing slash", "http://localhost:11434/v1/", "http://localhost:11434"},
		{"custom host with v1", "https://example.com:8080/v1", "https://example.com:8080"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := BaseURLToHostRoot(tc.baseURL); got != tc.want {
				t.Errorf("BaseURLToHostRoot(%q) = %q, want %q", tc.baseURL, got, tc.want)
			}
		})
	}
}
