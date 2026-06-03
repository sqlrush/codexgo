package appserverproto

import "testing"

func TestSerializationScopeConstructors(t *testing.T) {
	if NoScope() != nil {
		t.Fatal("NoScope must be nil")
	}

	cases := []struct {
		name string
		got  *SerializationScope
		kind SerializationScopeKind
		key  string
	}{
		{"global", GlobalScope("config"), ScopeGlobal, "config"},
		{"global-shared-read", GlobalSharedReadScope("config"), ScopeGlobalSharedRead, "config"},
		{"thread", ThreadScope("t1"), ScopeThread, "t1"},
		{"thread-path", ThreadPathScope("/r.jsonl"), ScopeThreadPath, "/r.jsonl"},
		{"command-exec", CommandExecProcessScope("p1"), ScopeCommandExecProcess, "p1"},
		{"process", ProcessScope("h1"), ScopeProcess, "h1"},
		{"fuzzy", FuzzyFileSearchSessionScope("s1"), ScopeFuzzyFileSearchSession, "s1"},
		{"fs-watch", FsWatchScope("w1"), ScopeFsWatch, "w1"},
		{"mcp-oauth", McpOauthScope("srv"), ScopeMcpOauth, "srv"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got == nil {
				t.Fatal("scope is nil")
			}
			if tc.got.Kind != tc.kind || tc.got.Key != tc.key {
				t.Fatalf("got %+v, want kind=%d key=%q", *tc.got, tc.kind, tc.key)
			}
		})
	}
}
