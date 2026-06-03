package memories

import "testing"

func TestScopeFromPath(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", "root"},
		{"/", "root"},
		// "./" trims to "." (the trailing slash is removed before the "./" prefix
		// strip can apply), so it classifies as "other", matching scope_from_path.
		{"./", "other"},
		{"MEMORY.md", "memory_md"},
		{"memory_summary.md", "memory_summary"},
		{"raw_memories.md", "raw_memories"},
		{"rollout_summaries", "rollout_summaries"},
		{"rollout_summaries/foo.md", "rollout_summaries"},
		{"skills", "skills"},
		{"skills/foo/SKILL.md", "skills"},
		{"extensions/ad_hoc/notes", "ad_hoc_notes"},
		{"extensions/ad_hoc/notes/x.md", "ad_hoc_notes"},
		{"./MEMORY.md", "memory_md"},
		{"other/file.md", "other"},
	}
	for _, tc := range tests {
		if got := ScopeFromPath(tc.in); got != tc.want {
			t.Errorf("ScopeFromPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestScopeFromOptionalPath(t *testing.T) {
	if got := ScopeFromOptionalPath(nil, "all"); got != "all" {
		t.Errorf("nil scope = %q, want all", got)
	}
	p := "MEMORY.md"
	if got := ScopeFromOptionalPath(&p, "all"); got != "memory_md" {
		t.Errorf("scope = %q, want memory_md", got)
	}
}

func TestTruncatedTag(t *testing.T) {
	yes, no := true, false
	if got := TruncatedTag(nil); got != "unknown" {
		t.Errorf("nil tag = %q", got)
	}
	if got := TruncatedTag(&yes); got != "true" {
		t.Errorf("true tag = %q", got)
	}
	if got := TruncatedTag(&no); got != "false" {
		t.Errorf("false tag = %q", got)
	}
}

func TestStatusTag(t *testing.T) {
	if got := StatusTag(true); got != "succeeded" {
		t.Errorf("success tag = %q", got)
	}
	if got := StatusTag(false); got != "failed" {
		t.Errorf("failed tag = %q", got)
	}
}
