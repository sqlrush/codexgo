package cli

import "testing"

func TestCanonicalSubcommand(t *testing.T) {
	tests := []struct {
		token     string
		wantName  string
		wantKnown bool
	}{
		{"exec", "exec", true},
		{"e", "exec", true},
		{"apply", "apply", true},
		{"a", "apply", true},
		{"mcp-server", "mcp-server", true},
		{"doctor", "doctor", true},
		{"unknown", "", false},
		{"prompt text", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			name, known := canonicalSubcommand(tt.token)
			if name != tt.wantName || known != tt.wantKnown {
				t.Errorf("canonicalSubcommand(%q) = (%q, %v), want (%q, %v)", tt.token, name, known, tt.wantName, tt.wantKnown)
			}
		})
	}
}

func TestEveryHandlerHasSummaryAndAlias(t *testing.T) {
	// Every dispatched handler must be reachable via the alias map so the parser
	// can route it.
	for name := range handlers {
		if _, ok := subcommandAliases[name]; !ok {
			t.Errorf("handler %q has no alias entry", name)
		}
	}
	// Every alias must resolve to a registered handler.
	for token, canonical := range subcommandAliases {
		if _, ok := handlers[canonical]; !ok {
			t.Errorf("alias %q -> %q has no handler", token, canonical)
		}
	}
}
