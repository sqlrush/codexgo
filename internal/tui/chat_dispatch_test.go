package tui

import "testing"

func TestParseInputForDispatchPlainText(t *testing.T) {
	got := ParseInputForDispatch("hello there")
	if got.IsSlash || got.Text != "hello there" {
		t.Fatalf("plain text misclassified: %+v", got)
	}
}

func TestParseInputForDispatchSlash(t *testing.T) {
	got := ParseInputForDispatch("/compact")
	if !got.IsSlash || got.Command != SlashCompact {
		t.Fatalf("/compact = %+v, want SlashCompact", got)
	}
}

func TestParseInputForDispatchSlashWithArgs(t *testing.T) {
	got := ParseInputForDispatch("/review focus on tests")
	if !got.IsSlash || got.Command != SlashReview {
		t.Fatalf("/review = %+v", got)
	}
	if got.Args != "focus on tests" {
		t.Fatalf("args = %q, want 'focus on tests'", got.Args)
	}
}

func TestParseInputForDispatchUnknownSlashIsText(t *testing.T) {
	got := ParseInputForDispatch("/notacommand")
	if got.IsSlash {
		t.Fatalf("unknown slash should be plain text: %+v", got)
	}
	if got.Text != "/notacommand" {
		t.Fatalf("text = %q", got.Text)
	}
}

func TestParseInputBareSlashIsText(t *testing.T) {
	got := ParseInputForDispatch("/")
	if got.IsSlash {
		t.Fatalf("bare slash should be text: %+v", got)
	}
}
