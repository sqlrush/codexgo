package secrets

import "testing"

func TestRedactSecrets(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "load regex compiles on empty-ish input",
			input: "secret",
			want:  "secret",
		},
		{
			name:  "openai key",
			input: "key=sk-abcdefghijklmnopqrstuvwxyz0123",
			want:  "key=[REDACTED_SECRET]",
		},
		{
			name:  "aws access key id",
			input: "id AKIAIOSFODNN7EXAMPLE done",
			want:  "id [REDACTED_SECRET] done",
		},
		{
			name:  "bearer token",
			input: "Authorization: Bearer abcdefghijklmnop1234",
			want:  "Authorization: Bearer [REDACTED_SECRET]",
		},
		{
			name:  "bearer token case insensitive",
			input: "bearer abcdefghijklmnop1234",
			want:  "Bearer [REDACTED_SECRET]",
		},
		{
			name:  "secret assignment with quotes",
			input: `api_key: "supersecretvalue"`,
			want:  `api_key: "[REDACTED_SECRET]"`,
		},
		{
			name:  "secret assignment without quotes",
			input: "password=supersecretvalue",
			want:  "password=[REDACTED_SECRET]",
		},
		{
			name:  "token assignment",
			input: "token = mytokenvalue123",
			want:  "token = [REDACTED_SECRET]",
		},
		{
			name:  "no match preserves input",
			input: "just some normal text",
			want:  "just some normal text",
		},
		{
			name:  "short value not redacted",
			input: "password=short",
			want:  "password=short",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RedactSecrets(tt.input); got != tt.want {
				t.Fatalf("RedactSecrets(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
