package execpolicy

import (
	"reflect"
	"testing"
)

func TestShlexSplit(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
		ok    bool
	}{
		{"simple", "git status", []string{"git", "status"}, true},
		{"extra spaces", "  cp   -r  src dest ", []string{"cp", "-r", "src", "dest"}, true},
		{"single quotes", `echo 'hello world'`, []string{"echo", "hello world"}, true},
		{"double quotes", `echo "hello world"`, []string{"echo", "hello world"}, true},
		{"escaped space", `echo a\ b`, []string{"echo", "a b"}, true},
		{"empty quoted word", `echo ""`, []string{"echo", ""}, true},
		{"double-quote escape", `echo "a\"b"`, []string{"echo", `a"b`}, true},
		{"config example", "git --config color.status=always status",
			[]string{"git", "--config", "color.status=always", "status"}, true},
		{"empty", "", nil, true},
		{"unterminated single quote", `echo 'oops`, nil, false},
		{"unterminated double quote", `echo "oops`, nil, false},
		{"trailing backslash", `echo \`, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := shlexSplit(tc.input)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestShlexTryJoin(t *testing.T) {
	cases := []struct {
		name   string
		tokens []string
		want   string
		ok     bool
	}{
		{"simple", []string{"git", "status"}, "git status", true},
		{"with spaces", []string{"echo", "hello world"}, "echo 'hello world'", true},
		{"empty token", []string{"echo", ""}, "echo ''", true},
		// '=' is not in shlex's safe set, so the token is single-quoted.
		{"equals is not safe", []string{"cat", "color.status=always"}, "cat 'color.status=always'", true},
		{"safe punctuation", []string{"cp", "src/a-b_c.txt"}, "cp src/a-b_c.txt", true},
		{"nul byte", []string{"echo", "a\x00b"}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := shlexTryJoin(tc.tokens)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
