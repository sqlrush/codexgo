package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func typeString(c Composer, s string) Composer {
	for _, r := range s {
		c = c.HandleKey(runeKey(r)).Composer
	}
	return c
}

func TestComposerTypeAndSubmit(t *testing.T) {
	c := NewComposer(testTheme(), nil)
	c = typeString(c, "hello world")
	if c.Text() != "hello world" {
		t.Fatalf("text = %q", c.Text())
	}
	res := c.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if res.Submit != "hello world" {
		t.Fatalf("submit = %q, want %q", res.Submit, "hello world")
	}
	if !res.Composer.IsEmpty() {
		t.Fatalf("composer not cleared after submit: %q", res.Composer.Text())
	}
}

func TestComposerAltEnterInsertsNewline(t *testing.T) {
	c := NewComposer(testTheme(), nil)
	c = typeString(c, "a")
	c = c.HandleKey(tea.KeyMsg{Type: tea.KeyEnter, Alt: true}).Composer
	c = typeString(c, "b")
	if c.Text() != "a\nb" {
		t.Fatalf("text = %q, want %q", c.Text(), "a\nb")
	}
}

func TestComposerBackspace(t *testing.T) {
	c := NewComposer(testTheme(), nil)
	c = typeString(c, "abc")
	c = c.HandleKey(tea.KeyMsg{Type: tea.KeyBackspace}).Composer
	if c.Text() != "ab" {
		t.Fatalf("text = %q, want ab", c.Text())
	}
}

func TestComposerTabQueuesWhenRunning(t *testing.T) {
	c := NewComposer(testTheme(), nil).SetTaskRunning(true)
	c = typeString(c, "queued message")
	res := c.HandleKey(tea.KeyMsg{Type: tea.KeyTab})
	if res.Submit != "queued message" || !res.Queue {
		t.Fatalf("tab submit=%q queue=%v, want queued/true", res.Submit, res.Queue)
	}
}

func TestComposerTabSubmitsWhenIdle(t *testing.T) {
	c := NewComposer(testTheme(), nil)
	c = typeString(c, "go now")
	res := c.HandleKey(tea.KeyMsg{Type: tea.KeyTab})
	if res.Submit != "go now" || res.Queue {
		t.Fatalf("tab idle submit=%q queue=%v, want go now/false", res.Submit, res.Queue)
	}
}

func TestComposerSlashPopupShows(t *testing.T) {
	c := NewComposer(testTheme(), nil)
	c = typeString(c, "/")
	if !c.PopupVisible() {
		t.Fatal("slash popup should be visible after typing '/'")
	}
	rows, _, ok := c.PopupRows()
	if !ok || len(rows) == 0 {
		t.Fatal("expected slash popup rows")
	}
}

func TestComposerFilePopupUsesSearch(t *testing.T) {
	called := ""
	search := func(q string) []string {
		called = q
		return []string{"src/main.go", "src/main_test.go"}
	}
	c := NewComposer(testTheme(), search)
	c = typeString(c, "see @main")
	if !c.PopupVisible() {
		t.Fatal("file popup should be visible")
	}
	if called != "main" {
		t.Fatalf("file search query = %q, want main", called)
	}
	rows, _, _ := c.PopupRows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 file rows, got %d", len(rows))
	}
}

func TestComposerAcceptFileMention(t *testing.T) {
	search := func(q string) []string { return []string{"src/main.go"} }
	c := NewComposer(testTheme(), search)
	c = typeString(c, "@mai")
	c = c.HandleKey(tea.KeyMsg{Type: tea.KeyEnter}).Composer
	if c.Text() != "@src/main.go " {
		t.Fatalf("text after accept = %q, want '@src/main.go '", c.Text())
	}
}

func TestComposerHistoryRecall(t *testing.T) {
	c := NewComposer(testTheme(), nil)
	c = typeString(c, "first")
	c = c.HandleKey(tea.KeyMsg{Type: tea.KeyEnter}).Composer
	c = typeString(c, "second")
	c = c.HandleKey(tea.KeyMsg{Type: tea.KeyEnter}).Composer

	// Up recalls newest first.
	c = c.HandleKey(tea.KeyMsg{Type: tea.KeyUp}).Composer
	if c.Text() != "second" {
		t.Fatalf("first up = %q, want second", c.Text())
	}
	c = c.HandleKey(tea.KeyMsg{Type: tea.KeyUp}).Composer
	if c.Text() != "first" {
		t.Fatalf("second up = %q, want first", c.Text())
	}
	// Down returns toward newer.
	c = c.HandleKey(tea.KeyMsg{Type: tea.KeyDown}).Composer
	if c.Text() != "second" {
		t.Fatalf("down = %q, want second", c.Text())
	}
}

func TestComposerReverseSearch(t *testing.T) {
	c := NewComposer(testTheme(), nil)
	for _, m := range []string{"git status", "cargo build", "git diff"} {
		c = typeString(c, m)
		c = c.HandleKey(tea.KeyMsg{Type: tea.KeyEnter}).Composer
	}
	c = c.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlR}).Composer
	if !c.SearchActive() {
		t.Fatal("Ctrl+R should activate reverse search")
	}
	c = typeString(c, "git")
	_, preview, _ := c.CurrentSearch()
	if preview != "git diff" {
		t.Fatalf("search preview = %q, want 'git diff'", preview)
	}
	// Ctrl+R again moves to next older match.
	c = c.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlR}).Composer
	_, preview2, _ := c.CurrentSearch()
	if preview2 != "git status" {
		t.Fatalf("second match = %q, want 'git status'", preview2)
	}
	// Enter accepts the preview.
	c = c.HandleKey(tea.KeyMsg{Type: tea.KeyEnter}).Composer
	if c.SearchActive() {
		t.Fatal("Enter should close search")
	}
	if c.Text() != "git status" {
		t.Fatalf("accepted = %q, want 'git status'", c.Text())
	}
}
