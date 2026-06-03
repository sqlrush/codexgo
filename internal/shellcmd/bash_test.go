package shellcmd

import (
	"reflect"
	"testing"
)

// parseSeq parses a raw script into its word-only command sequence, mirroring
// the bash.rs `parse_seq` test helper.
func parseSeq(t *testing.T, src string) ([][]string, bool) {
	t.Helper()
	file, ok := parseShell(src)
	if !ok {
		return nil, false
	}
	return TryParseWordOnlyCommandsSequence(file)
}

func TestTryParseWordOnlyCommandsSequence(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want [][]string // nil means "rejected"
	}{
		{
			name: "accepts_single_simple_command",
			src:  "ls -1",
			want: [][]string{{"ls", "-1"}},
		},
		{
			name: "accepts_multiple_commands_with_allowed_operators",
			src:  "ls && pwd; echo 'hi there' | wc -l",
			want: [][]string{{"ls"}, {"pwd"}, {"echo", "hi there"}, {"wc", "-l"}},
		},
		{
			name: "extracts_double_quoted_string",
			src:  `echo "hello world"`,
			want: [][]string{{"echo", "hello world"}},
		},
		{
			name: "extracts_single_quoted_string",
			src:  "echo 'hi there'",
			want: [][]string{{"echo", "hi there"}},
		},
		{
			name: "accepts_double_quoted_strings_with_newlines",
			src:  "git commit -m \"line1\nline2\"",
			want: [][]string{{"git", "commit", "-m", "line1\nline2"}},
		},
		{
			name: "accepts_mixed_quote_concatenation_a",
			src:  `echo "/usr"'/'"local"/bin`,
			want: [][]string{{"echo", "/usr/local/bin"}},
		},
		{
			name: "accepts_mixed_quote_concatenation_b",
			src:  `echo '/usr'"/"'local'/bin`,
			want: [][]string{{"echo", "/usr/local/bin"}},
		},
		{
			name: "accepts_numbers_as_words",
			src:  "echo 123 456",
			want: [][]string{{"echo", "123", "456"}},
		},
		{
			name: "accepts_concatenated_flag_and_value",
			src:  `rg -n "foo" -g"*.py"`,
			want: [][]string{{"rg", "-n", "foo", "-g*.py"}},
		},
		{
			name: "accepts_concatenated_flag_with_single_quotes",
			src:  "grep -n 'pattern' -g'*.txt'",
			want: [][]string{{"grep", "-n", "pattern", "-g*.txt"}},
		},
		{name: "rejects_double_quoted_expansion_braces", src: `echo "hi ${USER}"`, want: nil},
		{name: "rejects_double_quoted_expansion_var", src: `echo "$HOME"`, want: nil},
		{name: "rejects_parentheses", src: "(ls)", want: nil},
		{name: "rejects_subshell_in_or", src: "ls || (pwd && echo hi)", want: nil},
		{name: "rejects_redirection", src: "ls > out.txt", want: nil},
		{name: "rejects_background_operator", src: "echo hi & echo bye", want: nil},
		{name: "rejects_command_substitution_dollar", src: "echo $(pwd)", want: nil},
		{name: "rejects_command_substitution_backtick", src: "echo `pwd`", want: nil},
		{name: "rejects_variable_expansion", src: "echo $HOME", want: nil},
		{name: "rejects_double_quoted_var", src: `echo "hi $USER"`, want: nil},
		{name: "rejects_variable_assignment_prefix", src: "FOO=bar ls", want: nil},
		{name: "rejects_trailing_operator_parse_error", src: "ls &&", want: nil},
		{name: "rejects_leading_operator", src: "&& ls", want: nil},
		{name: "rejects_double_separator", src: "ls ;; pwd", want: nil},
		{name: "rejects_empty_pipeline_segment", src: "ls | | wc", want: nil},
		{name: "rejects_concat_with_var_subst_dq", src: `rg -g"$VAR" pattern`, want: nil},
		{name: "rejects_concat_with_var_subst_braces", src: `rg -g"${VAR}" pattern`, want: nil},
		{name: "rejects_concat_with_cmd_subst", src: `rg -g"$(pwd)" pattern`, want: nil},
		{name: "rejects_concat_with_nested_cmd_subst", src: `rg -g"$(echo '*.py')" pattern`, want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseSeq(t, tc.src)
			if tc.want == nil {
				if ok {
					t.Fatalf("expected rejection, got %v", got)
				}
				return
			}
			if !ok {
				t.Fatalf("expected acceptance, got rejection")
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseShellLcPlainCommands(t *testing.T) {
	tests := []struct {
		name    string
		command []string
		want    [][]string
		wantOK  bool
	}{
		{
			name:    "parse_zsh_lc_plain_commands",
			command: []string{"zsh", "-lc", "ls"},
			want:    [][]string{{"ls"}},
			wantOK:  true,
		},
		{
			name:    "parse_bash_c_plain_commands",
			command: []string{"bash", "-c", "ls -1 && pwd"},
			want:    [][]string{{"ls", "-1"}, {"pwd"}},
			wantOK:  true,
		},
		{
			name:    "rejects_non_shell",
			command: []string{"python", "-lc", "ls"},
			wantOK:  false,
		},
		{
			name:    "rejects_wrong_flag",
			command: []string{"bash", "-x", "ls"},
			wantOK:  false,
		},
		{
			name:    "rejects_wrong_arity",
			command: []string{"bash", "-lc"},
			wantOK:  false,
		},
		{
			name:    "rejects_script_with_redirect",
			command: []string{"bash", "-lc", "echo hi > out.txt"},
			wantOK:  false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseShellLcPlainCommands(tc.command)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (got %v)", ok, tc.wantOK, got)
			}
			if tc.wantOK && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExtractBashCommand(t *testing.T) {
	tests := []struct {
		name       string
		command    []string
		wantShell  string
		wantScript string
		wantOK     bool
	}{
		{"bash_lc", []string{"bash", "-lc", "ls"}, "bash", "ls", true},
		{"zsh_c", []string{"zsh", "-c", "pwd"}, "zsh", "pwd", true},
		{"sh_lc", []string{"sh", "-lc", "echo hi"}, "sh", "echo hi", true},
		{"abs_path_bin_bash", []string{"/bin/bash", "-lc", "ls"}, "/bin/bash", "ls", true},
		{"powershell_rejected", []string{"pwsh", "-c", "Get-ChildItem"}, "", "", false},
		{"wrong_flag", []string{"bash", "-x", "ls"}, "", "", false},
		{"too_few", []string{"bash", "-lc"}, "", "", false},
		{"too_many", []string{"bash", "-lc", "ls", "extra"}, "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			shell, script, ok := ExtractBashCommand(tc.command)
			if ok != tc.wantOK || shell != tc.wantShell || script != tc.wantScript {
				t.Fatalf("got (%q, %q, %v), want (%q, %q, %v)",
					shell, script, ok, tc.wantShell, tc.wantScript, tc.wantOK)
			}
		})
	}
}

func TestIsLoginShellFlag(t *testing.T) {
	tests := []struct {
		flag string
		want bool
	}{
		{"-lc", true},
		{"-c", false},
		{"-l", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := IsLoginShellFlag(tc.flag); got != tc.want {
			t.Errorf("IsLoginShellFlag(%q) = %v, want %v", tc.flag, got, tc.want)
		}
	}
}
