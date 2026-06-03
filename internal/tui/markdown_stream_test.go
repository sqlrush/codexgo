package tui

import "testing"

func TestStreamCollectorNoCommitUntilNewline(t *testing.T) {
	c := NewMarkdownStreamCollector()
	c.PushDelta("Hello, world")
	if _, ok := c.CommitCompleteSource(); ok {
		t.Fatal("should not commit without a newline")
	}
	c.PushDelta("!\n")
	out, ok := c.CommitCompleteSource()
	if !ok {
		t.Fatal("expected a commit after newline")
	}
	if out != "Hello, world!\n" {
		t.Fatalf("commit = %q, want %q", out, "Hello, world!\n")
	}
}

func TestStreamCollectorIncrementalCommits(t *testing.T) {
	c := NewMarkdownStreamCollector()
	c.PushDelta("line one\n")
	first, _ := c.CommitCompleteSource()
	if first != "line one\n" {
		t.Fatalf("first commit = %q", first)
	}
	c.PushDelta("line two\n")
	second, _ := c.CommitCompleteSource()
	if second != "line two\n" {
		t.Fatalf("second commit = %q", second)
	}
	if got := c.CommittedSource(); got != "line one\nline two\n" {
		t.Fatalf("committed source = %q", got)
	}
}

func TestStreamCollectorFinalizeAddsNewline(t *testing.T) {
	c := NewMarkdownStreamCollector()
	c.PushDelta("no trailing newline")
	out := c.FinalizeAndDrainSource()
	if out != "no trailing newline\n" {
		t.Fatalf("finalize = %q", out)
	}
	// After finalize the collector is cleared.
	if c.CommittedSource() != "" {
		t.Fatalf("committed source not cleared: %q", c.CommittedSource())
	}
}

func TestStreamCollectorFinalizeAfterCommit(t *testing.T) {
	c := NewMarkdownStreamCollector()
	c.PushDelta("committed\n")
	c.CommitCompleteSource()
	c.PushDelta("tail")
	out := c.FinalizeAndDrainSource()
	if out != "tail\n" {
		t.Fatalf("finalize tail = %q, want %q", out, "tail\n")
	}
}
