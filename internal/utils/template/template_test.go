package template

import (
	"errors"
	"reflect"
	"testing"
)

func TestRenderReplacesPlaceholdersWithAndWithoutWhitespace(t *testing.T) {
	got, err := Render(
		"Hello, {{ name }}. You are in {{place}}. {{ name }} is repeated.",
		[]Pair{{"name", "Codex"}, {"place", "codex-rs"}},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Hello, Codex. You are in codex-rs. Codex is repeated."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParsedTemplatesCanBeReused(t *testing.T) {
	tmpl, err := Parse("{{greeting}}, {{ name }}!")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	cases := []struct {
		pairs []Pair
		want  string
	}{
		{[]Pair{{"greeting", "Hello"}, {"name", "Codex"}}, "Hello, Codex!"},
		{[]Pair{{"greeting", "Hi"}, {"name", "builder"}}, "Hi, builder!"},
	}
	for _, tc := range cases {
		got, err := tmpl.RenderPairs(tc.pairs)
		if err != nil {
			t.Fatalf("unexpected render error: %v", err)
		}
		if got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
}

func TestPlaceholdersAreSortedAndUnique(t *testing.T) {
	tmpl, err := Parse("{{ b }} {{ a }} {{ b }}")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	got := tmpl.Placeholders()
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPlaceholdersReturnsDefensiveCopy(t *testing.T) {
	tmpl, err := Parse("{{ a }} {{ b }}")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	first := tmpl.Placeholders()
	first[0] = "mutated"
	second := tmpl.Placeholders()
	if second[0] != "a" {
		t.Errorf("Placeholders mutated internal state: got %v", second)
	}
}

func TestRenderSupportsMultilineAndAdjacentPlaceholders(t *testing.T) {
	got, err := Render(
		"Line 1: {{first}}{{second}}\nLine 2: {{ third }}",
		[]Pair{{"first", "A"}, {"second", "B"}, {"third", "C"}},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Line 1: AB\nLine 2: C"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderSupportsLiteralDelimiterEscapes(t *testing.T) {
	got, err := Render(
		"literal open: {{{{, literal close: }}}}, value: {{ name }}",
		[]Pair{{"name", "Codex"}},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "literal open: {{, literal close: }}, value: Codex"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadmeExample(t *testing.T) {
	tmpl, err := Parse(
		"Hello, {{ name }}.\nLiteral braces: {{{{ and }}}}.\nMode: {{ mode }}",
	)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	got, err := tmpl.RenderPairs([]Pair{{"name", "Codex"}, {"mode", "strict"}})
	if err != nil {
		t.Fatalf("unexpected render error: %v", err)
	}
	want := "Hello, Codex.\nLiteral braces: {{ and }}.\nMode: strict"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	oneShot, err := Render("Hi {{ who }}!", []Pair{{"who", "there"}})
	if err != nil {
		t.Fatalf("unexpected one-shot error: %v", err)
	}
	if oneShot != "Hi there!" {
		t.Errorf("got %q, want %q", oneShot, "Hi there!")
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		wantStart int
		check     func(*ParseError) bool
		message   string
	}{
		{
			name:      "empty placeholder",
			source:    "Hello, {{   }}.",
			wantStart: 7,
			check:     (*ParseError).IsEmptyPlaceholder,
			message:   "template placeholder at byte 7 is empty",
		},
		{
			name:      "unterminated placeholder",
			source:    "Hello, {{ name.",
			wantStart: 7,
			check:     (*ParseError).IsUnterminatedPlaceholder,
			message:   "template placeholder starting at byte 7 is missing `}}`",
		},
		{
			name:      "nested placeholder",
			source:    "Hello, {{ outer {{ inner }} }}.",
			wantStart: 7,
			check:     (*ParseError).IsNestedPlaceholder,
			message:   "template placeholder starting at byte 7 contains a nested `{{`",
		},
		{
			name:      "unmatched closing delimiter",
			source:    "Hello, }} world.",
			wantStart: 7,
			check:     (*ParseError).IsUnmatchedClosingDelimiter,
			message:   "template contains an unmatched `}}` at byte 7",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.source)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			var pe *ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("expected *ParseError, got %T", err)
			}
			if !tc.check(pe) {
				t.Errorf("error kind mismatch: %v", pe)
			}
			if pe.Start != tc.wantStart {
				t.Errorf("Start = %d, want %d", pe.Start, tc.wantStart)
			}
			if pe.Error() != tc.message {
				t.Errorf("message = %q, want %q", pe.Error(), tc.message)
			}
		})
	}
}

func TestRenderErrors(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		pairs    []Pair
		check    func(*RenderError) bool
		wantName string
		message  string
	}{
		{
			name:     "missing value",
			source:   "Hello, {{ name }}.",
			pairs:    nil,
			check:    (*RenderError).IsMissingValue,
			wantName: "name",
			message:  "template placeholder `name` is missing a value",
		},
		{
			name:     "extra value",
			source:   "Hello, {{ name }}.",
			pairs:    []Pair{{"name", "Codex"}, {"unused", "extra"}},
			check:    (*RenderError).IsExtraValue,
			wantName: "unused",
			message:  "template value `unused` is not used by this template",
		},
		{
			name:     "duplicate value",
			source:   "Hello, {{ name }}.",
			pairs:    []Pair{{"name", "Codex"}, {"name", "other"}},
			check:    (*RenderError).IsDuplicateValue,
			wantName: "name",
			message:  "template value `name` was provided more than once",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := Parse(tc.source)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			_, err = tmpl.RenderPairs(tc.pairs)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			var re *RenderError
			if !errors.As(err, &re) {
				t.Fatalf("expected *RenderError, got %T", err)
			}
			if !tc.check(re) {
				t.Errorf("error kind mismatch: %v", re)
			}
			if re.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", re.Name, tc.wantName)
			}
			if re.Error() != tc.message {
				t.Errorf("message = %q, want %q", re.Error(), tc.message)
			}
		})
	}
}

func TestRenderFunctionWrapsParseErrors(t *testing.T) {
	_, err := Render("Hello, }} world.", []Pair{{"name", "Codex"}})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var te *Error
	if !errors.As(err, &te) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if !te.IsParse() {
		t.Errorf("expected wrapped parse error, got %v", te.Cause)
	}
	var pe *ParseError
	if !errors.As(err, &pe) || !pe.IsUnmatchedClosingDelimiter() || pe.Start != 7 {
		t.Errorf("unexpected wrapped parse error: %v", err)
	}
}

func TestRenderFunctionWrapsRenderErrors(t *testing.T) {
	_, err := Render("Hello, {{ name }}.", []Pair{{"extra", "Codex"}})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var te *Error
	if !errors.As(err, &te) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if !te.IsRender() {
		t.Errorf("expected wrapped render error, got %v", te.Cause)
	}
	// Render checks missing values before extra values, so the missing `name`
	// placeholder is reported first.
	var re *RenderError
	if !errors.As(err, &re) || !re.IsMissingValue() || re.Name != "name" {
		t.Errorf("unexpected wrapped render error: %v", err)
	}
}

func TestRenderMap(t *testing.T) {
	got, err := RenderMap(
		"Hello, {{ name }}.",
		map[string]string{"name": "Codex"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Hello, Codex." {
		t.Errorf("got %q, want %q", got, "Hello, Codex.")
	}
}

func TestRenderDoesNotMutateInput(t *testing.T) {
	tmpl, err := Parse("{{ a }} {{ b }}")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	vars := map[string]string{"a": "1", "b": "2"}
	if _, err := tmpl.Render(vars); err != nil {
		t.Fatalf("unexpected render error: %v", err)
	}
	want := map[string]string{"a": "1", "b": "2"}
	if !reflect.DeepEqual(vars, want) {
		t.Errorf("input map mutated: got %v, want %v", vars, want)
	}
}

func TestExtraValueIsDeterministic(t *testing.T) {
	tmpl, err := Parse("{{ used }}")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	// Two unused values; sorted iteration must report "aaa" before "zzz".
	_, err = tmpl.Render(map[string]string{"used": "x", "zzz": "1", "aaa": "2"})
	var re *RenderError
	if !errors.As(err, &re) || !re.IsExtraValue() || re.Name != "aaa" {
		t.Errorf("expected ExtraValue for `aaa`, got %v", err)
	}
}

func TestUnicodePlaceholders(t *testing.T) {
	// Multi-byte content surrounding placeholders must not corrupt offsets or
	// output.
	got, err := Render("héllo {{ name }} wörld", []Pair{{"name", "Codéx"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "héllo Codéx wörld"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestEmptyTemplate(t *testing.T) {
	got, err := Render("", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}
