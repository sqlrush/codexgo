package gitutils

import (
	"reflect"
	"testing"
)

func TestExtractPathsFromPatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		diff string
		want []string
	}{
		{
			name: "quoted headers",
			diff: "diff --git \"a/hello world.txt\" \"b/hello world.txt\"\nnew file mode 100644\n--- /dev/null\n+++ b/hello world.txt\n@@ -0,0 +1 @@\n+hi\n",
			want: []string{"hello world.txt"},
		},
		{
			name: "ignores dev null header",
			diff: "diff --git a/dev/null b/ok.txt\nnew file mode 100644\n--- /dev/null\n+++ b/ok.txt\n@@ -0,0 +1 @@\n+hi\n",
			want: []string{"ok.txt"},
		},
		{
			name: "unescapes c style tab in quoted headers",
			diff: "diff --git \"a/hello\\tworld.txt\" \"b/hello\\tworld.txt\"\nnew file mode 100644\n--- /dev/null\n+++ b/hello\tworld.txt\n@@ -0,0 +1 @@\n+hi\n",
			want: []string{"hello\tworld.txt"},
		},
		{
			name: "plain modify uses both sides deduped",
			diff: "diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-a\n+b\n",
			want: []string{"file.txt"},
		},
		{
			name: "rename yields both paths sorted",
			diff: "diff --git a/old.txt b/new.txt\nsimilarity index 100%\nrename from old.txt\nrename to new.txt\n",
			want: []string{"new.txt", "old.txt"},
		},
		{
			name: "no diff headers",
			diff: "not a diff\njust text\n",
			want: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractPathsFromPatch(tc.diff)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestUnescapeCString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{`plain`, "plain"},
		{`tab\there`, "tab\there"},
		{`new\nline`, "new\nline"},
		{`ret\rurn`, "ret\rurn"},
		{`back\\slash`, `back\slash`},
		{`quote\"end`, `quote"end`},
		{`octal\101`, "octalA"}, // \101 == 'A'
		{`bell\a`, "bell\a"},
		{`trailing\`, `trailing\`},
		{`unknown\z`, "unknownz"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := unescapeCString(tc.in); got != tc.want {
				t.Fatalf("unescapeCString(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
