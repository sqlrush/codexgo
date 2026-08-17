package core

// Tests for the tool_search advertisement gating, porting spec_plan's
// append_tool_search_executor: the tool advertises only when the model
// supports the search tool, namespace tools are available, and at least one
// deferred-exposure source exists (the collab agent tools in a bare run).

import (
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/pkg/features"
	"github.com/sqlrush/codexgo/pkg/modelsmanager"
)

// newSearchToolTurn builds a turn whose model supports the search tool.
func newSearchToolTurn(t *testing.T) *TurnContext {
	t.Helper()
	turn := newTestTurn(t.TempDir())
	turn.ModelInfo = modelsmanager.ModelInfo{Slug: "gpt-5.5", SupportsSearchTool: true}
	return turn
}

// TestToolSearchAdvertisedBeforeHostedSpecs asserts tool_search appears after
// the runtime tools and before the hosted web_search spec, like codex.
func TestToolSearchAdvertisedBeforeHostedSpecs(t *testing.T) {
	router, err := BuiltinToolRouter(BuiltinToolDeps{ShellTools: []ToolExecutor{namedExecutor{"shell_command"}}})
	if err != nil {
		t.Fatalf("build router: %v", err)
	}
	names := specNames(t, router, newSearchToolTurn(t))
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "tool_search,web_search") {
		t.Errorf("advertised order %q should end with tool_search,web_search", joined)
	}
}

// TestProviderCapabilitiesGateHostedTools asserts the provider capability upper
// bounds gate the namespace tool_search surface and the hosted web_search spec:
// a capabilities-off provider hides both, while the all-true default advertises
// them. Mirrors spec_plan reading turn_context.provider.capabilities().
func TestProviderCapabilitiesGateHostedTools(t *testing.T) {
	tests := []struct {
		name           string
		caps           ProviderCapabilities
		wantToolSearch bool
		wantWebSearch  bool
	}{
		{
			name:           "default (zero value) advertises both",
			caps:           ProviderCapabilities{},
			wantToolSearch: true,
			wantWebSearch:  true,
		},
		{
			name:           "all-true provider advertises both",
			caps:           ProviderCapabilities{NamespaceTools: true, ImageGeneration: true, WebSearch: true},
			wantToolSearch: true,
			wantWebSearch:  true,
		},
		{
			name:           "namespace-tools-off hides tool_search but keeps web_search",
			caps:           ProviderCapabilities{NamespaceTools: false, ImageGeneration: true, WebSearch: true},
			wantToolSearch: false,
			wantWebSearch:  true,
		},
		{
			name:           "web-search-off hides web_search but keeps tool_search",
			caps:           ProviderCapabilities{NamespaceTools: true, ImageGeneration: true, WebSearch: false},
			wantToolSearch: true,
			wantWebSearch:  false,
		},
		{
			name:           "bedrock-style provider hides web_search, keeps tool_search",
			caps:           ProviderCapabilities{NamespaceTools: true, ImageGeneration: false, WebSearch: false},
			wantToolSearch: true,
			wantWebSearch:  false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router, err := BuiltinToolRouter(BuiltinToolDeps{ShellTools: []ToolExecutor{namedExecutor{"shell_command"}}})
			if err != nil {
				t.Fatalf("build router: %v", err)
			}
			turn := newSearchToolTurn(t)
			turn.ProviderCapabilities = tc.caps
			names := strings.Join(specNames(t, router, turn), ",")

			if got := strings.Contains(names, "tool_search"); got != tc.wantToolSearch {
				t.Errorf("tool_search advertised = %v, want %v (names %s)", got, tc.wantToolSearch, names)
			}
			if got := strings.Contains(names, "web_search"); got != tc.wantWebSearch {
				t.Errorf("web_search advertised = %v, want %v (names %s)", got, tc.wantWebSearch, names)
			}
		})
	}
}

// TestToolSearchGating asserts the advertisement preconditions.
func TestToolSearchGating(t *testing.T) {
	noCollab := features.NewFeaturesWithDefaults()
	noCollab.SetEnabled(features.FeatureCollab, false)

	multiAgentV2 := features.NewFeaturesWithDefaults()
	multiAgentV2.SetEnabled(features.FeatureMultiAgentV2, true)

	tests := []struct {
		name   string
		mutate func(tc *TurnContext)
		want   bool
	}{
		{
			name:   "advertised for search-capable model with default features",
			mutate: func(tc *TurnContext) {},
			want:   true,
		},
		{
			name: "hidden when the model lacks search-tool support",
			mutate: func(tc *TurnContext) {
				tc.ModelInfo = modelsmanager.ModelInfo{Slug: "gpt-5.5", SupportsSearchTool: false}
			},
			want: false,
		},
		{
			name:   "hidden when collab tools are disabled (no deferred sources)",
			mutate: func(tc *TurnContext) { tc.Features = &noCollab },
			want:   false,
		},
		{
			name:   "hidden under multi-agent v2 (collab tools become direct)",
			mutate: func(tc *TurnContext) { tc.Features = &multiAgentV2 },
			want:   false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router, err := BuiltinToolRouter(BuiltinToolDeps{ShellTools: []ToolExecutor{namedExecutor{"shell_command"}}})
			if err != nil {
				t.Fatalf("build router: %v", err)
			}
			turn := newSearchToolTurn(t)
			tc.mutate(turn)
			names := strings.Join(specNames(t, router, turn), ",")
			got := strings.Contains(names, "tool_search")
			if got != tc.want {
				t.Errorf("tool_search advertised = %v, want %v (names %s)", got, tc.want, names)
			}
		})
	}
}
