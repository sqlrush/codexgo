package plugins

import "testing"

func TestValidatePluginSegment(t *testing.T) {
	tests := []struct {
		name    string
		segment string
		kind    string
		wantErr string
	}{
		{"valid alnum", "github", "plugin name", ""},
		{"valid with dash underscore", "open-ai_2", "plugin name", ""},
		{"empty", "", "plugin name", "invalid plugin name: must not be empty"},
		{"space", "open ai", "marketplace name", "invalid marketplace name: only ASCII letters, digits, `_`, and `-` are allowed"},
		{"at sign", "a@b", "plugin name", "invalid plugin name: only ASCII letters, digits, `_`, and `-` are allowed"},
		{"unicode", "café", "plugin name", "invalid plugin name: only ASCII letters, digits, `_`, and `-` are allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePluginSegment(tt.segment, tt.kind)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("got %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestParsePluginID(t *testing.T) {
	tests := []struct {
		name            string
		key             string
		wantPlugin      string
		wantMarketplace string
		wantErr         string
	}{
		{"valid", "github@openai-curated", "github", "openai-curated", ""},
		{"rsplit last at rejects embedded at", "a@b@market", "", "", "invalid plugin name: only ASCII letters, digits, `_`, and `-` are allowed in `a@b@market`"},
		{"no at", "github", "", "", "invalid plugin key `github`; expected <plugin>@<marketplace>"},
		{"empty plugin", "@market", "", "", "invalid plugin key `@market`; expected <plugin>@<marketplace>"},
		{"empty marketplace", "plugin@", "", "", "invalid plugin key `plugin@`; expected <plugin>@<marketplace>"},
		{"invalid segment", "plug in@market", "", "", "invalid plugin name: only ASCII letters, digits, `_`, and `-` are allowed in `plug in@market`"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := ParsePluginID(tt.key)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("got err %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id.PluginName != tt.wantPlugin || id.MarketplaceName != tt.wantMarketplace {
				t.Fatalf("got %+v, want %s@%s", id, tt.wantPlugin, tt.wantMarketplace)
			}
		})
	}
}

func TestPluginIDAsKey(t *testing.T) {
	id, err := NewPluginID("github", "openai-curated")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := id.AsKey(); got != "github@openai-curated" {
		t.Fatalf("got %q", got)
	}
}

func TestParsePluginIDRoundTrip(t *testing.T) {
	id, err := ParsePluginID("github@openai-curated")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.AsKey() != "github@openai-curated" {
		t.Fatalf("round trip failed: %q", id.AsKey())
	}
}
