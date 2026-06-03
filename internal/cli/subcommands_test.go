package cli

import (
	"sort"
	"testing"
)

// expectedSubcommandSet is the canonical set of top-level subcommands codex
// 0.136.0 exposes (visible variants only; the hidden execpolicy /
// responses-api-proxy / stdio-to-uds internals are not part of the public set).
var expectedSubcommandSet = []string{
	"app",
	"app-server",
	"apply",
	"archive",
	"cloud",
	"completion",
	"debug",
	"doctor",
	"exec",
	"exec-server",
	"features",
	"fork",
	"help",
	"login",
	"logout",
	"mcp",
	"mcp-server",
	"plugin",
	"remote-control",
	"resume",
	"review",
	"sandbox",
	"unarchive",
	"update",
}

// TestRegisteredSubcommandSetMatchesCodex asserts the registered handler set is
// exactly the codex 0.136.0 top-level subcommand set, and that the documented
// visible aliases (e -> exec, a -> apply, cloud-tasks -> cloud) resolve.
func TestRegisteredSubcommandSetMatchesCodex(t *testing.T) {
	got := make([]string, 0, len(handlers))
	for name := range handlers {
		got = append(got, name)
	}
	sort.Strings(got)

	want := append([]string(nil), expectedSubcommandSet...)
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("registered subcommand count = %d, want %d\n got:  %v\n want: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("subcommand[%d] = %q, want %q (full got=%v)", i, got[i], want[i], got)
		}
	}

	// Aliases must resolve to their canonical handlers.
	aliasCases := map[string]string{
		"e":           "exec",
		"a":           "apply",
		"cloud-tasks": "cloud",
	}
	for alias, canonical := range aliasCases {
		name, ok := canonicalSubcommand(alias)
		if !ok || name != canonical {
			t.Errorf("alias %q resolved to (%q, %v), want (%q, true)", alias, name, ok, canonical)
		}
	}
}

// TestEverySubcommandHasSummary asserts every registered handler appears in the
// help/summary registry so the top-level help lists it.
func TestEverySubcommandHasSummary(t *testing.T) {
	summaries := make(map[string]bool, len(subcommandSummaries))
	for _, s := range subcommandSummaries {
		summaries[s.Name] = true
	}
	for name := range handlers {
		if !summaries[name] {
			t.Errorf("handler %q has no summary entry", name)
		}
	}
}

// TestEverySubcommandHasHelpPrinter asserts every registered handler has a help
// printer so `codex help <cmd>` works for all of them.
func TestEverySubcommandHasHelpPrinter(t *testing.T) {
	for name := range handlers {
		if _, ok := helpPrinters[name]; !ok {
			t.Errorf("handler %q has no help printer", name)
		}
	}
}

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
