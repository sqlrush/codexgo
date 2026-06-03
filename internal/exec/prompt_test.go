package exec

import (
	"io"
	"strings"
	"testing"
)

// TestResolveRootPrompt covers the default-session prompt resolution: positional
// used as-is, piped stdin appended, and stdin-only fallback.
func TestResolveRootPrompt(t *testing.T) {
	str := func(s string) *string { return &s }
	tests := []struct {
		name       string
		prompt     *string
		stdin      string
		isTerminal bool
		want       string
		wantErr    bool
	}{
		{
			name:       "positional_only_terminal",
			prompt:     str("do the thing"),
			isTerminal: true,
			want:       "do the thing",
		},
		{
			name:   "positional_with_piped_stdin_appended",
			prompt: str("summarize"),
			stdin:  "file contents",
			want:   "summarize\n\n<stdin>\nfile contents\n</stdin>",
		},
		{
			name:  "stdin_only_when_no_prompt",
			stdin: "prompt from pipe",
			want:  "prompt from pipe",
		},
		{
			name:       "no_prompt_terminal_errors",
			isTerminal: true,
			wantErr:    true,
		},
		{
			name:   "dash_forces_stdin",
			prompt: str("-"),
			stdin:  "forced stdin",
			want:   "forced stdin",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := promptEnv{
				stdin:           strings.NewReader(tc.stdin),
				stdinIsTerminal: tc.isTerminal,
				errOut:          io.Discard,
			}
			got, err := resolveRootPrompt(env, tc.prompt)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveRootPrompt: %v", err)
			}
			if got != tc.want {
				t.Fatalf("prompt:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// TestResolvePrompt covers the resume/review path: a non-dash positional is used
// verbatim and a missing prompt on a terminal is an error.
func TestResolvePrompt(t *testing.T) {
	str := func(s string) *string { return &s }
	env := func(stdin string, term bool) promptEnv {
		return promptEnv{stdin: strings.NewReader(stdin), stdinIsTerminal: term, errOut: io.Discard}
	}

	if got, err := resolvePrompt(env("", true), str("explicit")); err != nil || got != "explicit" {
		t.Fatalf("explicit prompt: got %q err %v", got, err)
	}
	if got, err := resolvePrompt(env("piped", false), nil); err != nil || got != "piped" {
		t.Fatalf("piped prompt: got %q err %v", got, err)
	}
	if _, err := resolvePrompt(env("", true), nil); err == nil {
		t.Fatal("missing prompt on terminal should error")
	}
}

// TestBuildInput verifies images precede the text item in the assembled input.
func TestBuildInput(t *testing.T) {
	input := buildInput([]string{"a.png", "b.png"}, "describe")
	if len(input) != 3 {
		t.Fatalf("want 3 input items, got %d", len(input))
	}
	if input[0].Kind != "localImage" || input[0].Path != "a.png" {
		t.Fatalf("first item should be local image a.png, got %+v", input[0])
	}
	if input[2].Kind != "text" || input[2].Text != "describe" {
		t.Fatalf("last item should be the text prompt, got %+v", input[2])
	}
}
