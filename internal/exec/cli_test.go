package exec

import (
	"reflect"
	"testing"
)

// TestParseArgs covers flag and subcommand parsing across the supported surface.
func TestParseArgs(t *testing.T) {
	str := func(s string) *string { return &s }
	tests := []struct {
		name string
		args []string
		want CLI
	}{
		{
			name: "default_prompt",
			args: []string{"hello world"},
			want: CLI{Subcommand: SubcommandRun, Prompt: str("hello world")},
		},
		{
			name: "json_and_output_flags",
			args: []string{"--json", "-o", "out.txt", "--output-schema", "schema.json", "do it"},
			want: CLI{Subcommand: SubcommandRun, JSON: true, OutputLastMessage: "out.txt", OutputSchema: "schema.json", Prompt: str("do it")},
		},
		{
			name: "equals_forms",
			args: []string{"--output-last-message=last.txt", "--output-schema=s.json", "go"},
			want: CLI{Subcommand: SubcommandRun, OutputLastMessage: "last.txt", OutputSchema: "s.json", Prompt: str("go")},
		},
		{
			name: "images_csv_and_repeated",
			args: []string{"-i", "a.png,b.png", "--image", "c.png", "describe"},
			want: CLI{Subcommand: SubcommandRun, Images: []string{"a.png", "b.png", "c.png"}, Prompt: str("describe")},
		},
		{
			name: "experimental_json_alias",
			args: []string{"--experimental-json", "x"},
			want: CLI{Subcommand: SubcommandRun, JSON: true, Prompt: str("x")},
		},
		{
			name: "resume_with_session_and_prompt",
			args: []string{"resume", "sess-123", "continue"},
			want: CLI{Subcommand: SubcommandResume, ResumeSessionID: "sess-123", Prompt: str("continue")},
		},
		{
			name: "resume_last_with_prompt",
			args: []string{"resume", "--last", "keep going"},
			want: CLI{Subcommand: SubcommandResume, ResumeLast: true, Prompt: str("keep going")},
		},
		{
			name: "resume_session_only",
			args: []string{"resume", "sess-9"},
			want: CLI{Subcommand: SubcommandResume, ResumeSessionID: "sess-9"},
		},
		{
			name: "review_uncommitted",
			args: []string{"review", "--uncommitted"},
			want: CLI{Subcommand: SubcommandReview, ReviewUncommitted: true},
		},
		{
			name: "review_base",
			args: []string{"review", "--base", "main"},
			want: CLI{Subcommand: SubcommandReview, ReviewBase: "main"},
		},
		{
			name: "review_commit_with_title",
			args: []string{"review", "--commit", "abc123", "--title", "fix"},
			want: CLI{Subcommand: SubcommandReview, ReviewCommit: "abc123", ReviewTitle: "fix"},
		},
		{
			name: "review_custom_prompt",
			args: []string{"review", "check the auth flow"},
			want: CLI{Subcommand: SubcommandReview, Prompt: str("check the auth flow")},
		},
		{
			name: "unknown_flag_with_attached_value_tolerated",
			args: []string{"--model=gpt", "prompt"},
			want: CLI{Subcommand: SubcommandRun, Prompt: str("prompt")},
		},
		{
			name: "dash_prompt",
			args: []string{"-"},
			want: CLI{Subcommand: SubcommandRun, Prompt: str("-")},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseArgs(tc.args)
			if err != nil {
				t.Fatalf("ParseArgs: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseArgs mismatch:\n got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

// TestParseArgsErrors covers value-taking flags missing their argument.
func TestParseArgsErrors(t *testing.T) {
	cases := [][]string{
		{"-o"},
		{"--output-schema"},
		{"--image"},
		{"review", "--base"},
	}
	for _, args := range cases {
		if _, err := ParseArgs(args); err == nil {
			t.Fatalf("ParseArgs(%v) expected error", args)
		}
	}
}
