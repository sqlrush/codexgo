package modelsmanager

import "testing"

func mustRender(t *testing.T, source string, vars map[string]string) string {
	t.Helper()
	tmpl, err := parseTemplate(source)
	if err != nil {
		t.Fatalf("parse(%q): %v", source, err)
	}
	out, err := tmpl.render(vars)
	if err != nil {
		t.Fatalf("render(%q): %v", source, err)
	}
	return out
}

func TestTemplateRenderReplacesPlaceholders(t *testing.T) {
	out := mustRender(t,
		"Hello, {{ name }}. You are in {{place}}. {{ name }} is repeated.",
		map[string]string{"name": "Codex", "place": "codex-rs"},
	)
	want := "Hello, Codex. You are in codex-rs. Codex is repeated."
	if out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestTemplateLiteralEscapes(t *testing.T) {
	out := mustRender(t,
		"literal open: {{{{, literal close: }}}}, value: {{ name }}",
		map[string]string{"name": "Codex"},
	)
	want := "literal open: {{, literal close: }}, value: Codex"
	if out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestTemplateAdjacentPlaceholders(t *testing.T) {
	out := mustRender(t,
		"Line 1: {{first}}{{second}}\nLine 2: {{ third }}",
		map[string]string{"first": "A", "second": "B", "third": "C"},
	)
	want := "Line 1: AB\nLine 2: C"
	if out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestTemplatePlaceholdersSortedUnique(t *testing.T) {
	tmpl, err := parseTemplate("{{ b }} {{ a }} {{ b }}")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(tmpl.placeholders) != 2 || tmpl.placeholders[0] != "a" || tmpl.placeholders[1] != "b" {
		t.Fatalf("placeholders = %v, want [a b]", tmpl.placeholders)
	}
}

func TestTemplateParseErrors(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{"empty placeholder", "Hello, {{   }}."},
		{"unterminated", "Hello, {{ name."},
		{"nested", "Hello, {{ outer {{ inner }} }}."},
		{"unmatched close", "Hello, }} world."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseTemplate(tt.source); err == nil {
				t.Fatalf("expected parse error for %q", tt.source)
			}
		})
	}
}

func TestTemplateRenderErrors(t *testing.T) {
	tmpl, err := parseTemplate("Hello, {{ name }}.")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	tests := []struct {
		name string
		vars map[string]string
	}{
		{"missing value", map[string]string{}},
		{"extra value", map[string]string{"name": "Codex", "unused": "extra"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tmpl.render(tt.vars); err == nil {
				t.Fatalf("expected render error")
			}
		})
	}
}

func TestTemplateMultibyte(t *testing.T) {
	out := mustRender(t, "emoji 😀 {{ x }}", map[string]string{"x": "✓"})
	if out != "emoji 😀 ✓" {
		t.Fatalf("got %q", out)
	}
}
