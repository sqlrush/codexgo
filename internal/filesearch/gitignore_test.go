package filesearch

import "testing"

func TestCompilePattern(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantOK      bool
		wantNegate  bool
		wantDirOnly bool
		wantAnchor  bool
		wantSegs    []string
	}{
		{name: "blank", line: "   ", wantOK: false},
		{name: "comment", line: "# a comment", wantOK: false},
		{name: "simple name", line: "node_modules", wantOK: true, wantSegs: []string{"node_modules"}},
		{name: "negation", line: "!keep.txt", wantOK: true, wantNegate: true, wantSegs: []string{"keep.txt"}},
		{name: "dir only", line: "build/", wantOK: true, wantDirOnly: true, wantSegs: []string{"build"}},
		{name: "leading slash anchors", line: "/root.txt", wantOK: true, wantAnchor: true, wantSegs: []string{"root.txt"}},
		{name: "embedded slash anchors", line: "a/b.txt", wantOK: true, wantAnchor: true, wantSegs: []string{"a", "b.txt"}},
		{name: "doublestar", line: "**/x", wantOK: true, wantAnchor: true, wantSegs: []string{"**", "x"}},
		{name: "escaped comment", line: `\#real`, wantOK: true, wantSegs: []string{"#real"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ok := compilePattern(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if p.negate != tt.wantNegate {
				t.Errorf("negate = %v, want %v", p.negate, tt.wantNegate)
			}
			if p.dirOnly != tt.wantDirOnly {
				t.Errorf("dirOnly = %v, want %v", p.dirOnly, tt.wantDirOnly)
			}
			if p.anchored != tt.wantAnchor {
				t.Errorf("anchored = %v, want %v", p.anchored, tt.wantAnchor)
			}
			if !equalStrings(p.segments, tt.wantSegs) {
				t.Errorf("segments = %v, want %v", p.segments, tt.wantSegs)
			}
		})
	}
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{name: "literal match", pattern: "file.txt", input: "file.txt", want: true},
		{name: "literal mismatch", pattern: "file.txt", input: "other.txt", want: false},
		{name: "star suffix", pattern: "*.txt", input: "a.txt", want: true},
		{name: "star prefix", pattern: "file.*", input: "file.go", want: true},
		{name: "star middle", pattern: "a*z", input: "abcz", want: true},
		{name: "star empty", pattern: "a*z", input: "az", want: true},
		{name: "question", pattern: "f?o", input: "foo", want: true},
		{name: "question too short", pattern: "f?o", input: "fo", want: false},
		{name: "class match", pattern: "[abc].go", input: "b.go", want: true},
		{name: "class no match", pattern: "[abc].go", input: "d.go", want: false},
		{name: "class range", pattern: "[a-z]1", input: "m1", want: true},
		{name: "class negated", pattern: "[!x]y", input: "ay", want: true},
		{name: "class negated hit", pattern: "[!x]y", input: "xy", want: false},
		{name: "double star literal", pattern: "**", input: "anything", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchGlob(tt.pattern, tt.input); got != tt.want {
				t.Errorf("matchGlob(%q,%q) = %v, want %v", tt.pattern, tt.input, got, tt.want)
			}
		})
	}
}

func TestPatternMatches(t *testing.T) {
	mk := func(line string) gitignorePattern {
		p, _ := compilePattern(line)
		return p
	}
	tests := []struct {
		name string
		line string
		rel  string
		want bool
	}{
		{name: "unanchored name at root", line: "node_modules", rel: "node_modules", want: true},
		{name: "unanchored name nested", line: "node_modules", rel: "a/node_modules", want: true},
		{name: "unanchored covers children", line: "node_modules", rel: "node_modules/pkg/x.js", want: true},
		{name: "anchored at root only", line: "/build", rel: "build", want: true},
		{name: "anchored nested miss", line: "/build", rel: "a/build", want: false},
		{name: "embedded slash anchored", line: "a/b", rel: "a/b", want: true},
		{name: "embedded slash anchored miss", line: "a/b", rel: "x/a/b", want: false},
		{name: "doublestar anywhere", line: "**/target", rel: "deep/nested/target", want: true},
		{name: "glob extension", line: "*.log", rel: "errors.log", want: true},
		{name: "glob extension nested", line: "*.log", rel: "logs/errors.log", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := patternMatches(mk(tt.line), tt.rel); got != tt.want {
				t.Errorf("patternMatches(%q,%q) = %v, want %v", tt.line, tt.rel, got, tt.want)
			}
		})
	}
}

func TestIgnoredStackNegation(t *testing.T) {
	excludeAll, _ := loadGitignoreFromLines("", "*", "!keep.txt")
	tests := []struct {
		name  string
		rel   string
		isDir bool
		want  bool
	}{
		{name: "wildcard excludes", rel: "drop.txt", want: true},
		{name: "negation re-includes", rel: "keep.txt", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ignored([]gitignore{excludeAll}, tt.rel, tt.isDir)
			if got != tt.want {
				t.Errorf("ignored(%q) = %v, want %v", tt.rel, got, tt.want)
			}
		})
	}
}

// loadGitignoreFromLines builds a gitignore from in-memory lines for testing.
func loadGitignoreFromLines(base string, lines ...string) (gitignore, bool) {
	gi := gitignore{base: base}
	for _, l := range lines {
		if p, ok := compilePattern(l); ok {
			gi.patterns = append(gi.patterns, p)
		}
	}
	return gi, true
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
