package plugins

import "testing"

func TestProductFromSessionSourceName(t *testing.T) {
	tests := []struct {
		in   string
		want Product
		ok   bool
	}{
		{"chatgpt", ProductChatgpt, true},
		{"  Codex ", ProductCodex, true},
		{"ATLAS", ProductAtlas, true},
		{"unknown", 0, false},
	}
	for _, tt := range tests {
		got, ok := ProductFromSessionSourceName(tt.in)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("ProductFromSessionSourceName(%q)=%v,%v want %v,%v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestProductMatchesRestriction(t *testing.T) {
	if !ProductCodex.MatchesProductRestriction(nil) {
		t.Fatal("empty restriction should match")
	}
	if !ProductCodex.MatchesProductRestriction([]Product{ProductChatgpt, ProductCodex}) {
		t.Fatal("codex should match when listed")
	}
	if ProductCodex.MatchesProductRestriction([]Product{ProductChatgpt}) {
		t.Fatal("codex should not match when absent")
	}
}

func TestProductToAppPlatform(t *testing.T) {
	if ProductChatgpt.ToAppPlatform() != "chat" {
		t.Fatal("chatgpt -> chat")
	}
	if ProductCodex.ToAppPlatform() != "codex" {
		t.Fatal("codex -> codex")
	}
	if ProductAtlas.ToAppPlatform() != "atlas" {
		t.Fatal("atlas -> atlas")
	}
}

func TestParseProductAliases(t *testing.T) {
	for _, in := range []string{"chatgpt", "CHATGPT"} {
		if p, ok := parseProduct(in); !ok || p != ProductChatgpt {
			t.Errorf("parseProduct(%q) failed", in)
		}
	}
	if _, ok := parseProduct("nope"); ok {
		t.Error("parseProduct should reject unknown")
	}
}
