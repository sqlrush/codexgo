package plugins

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
	"github.com/sqlrush/codexgo/internal/utils/pluginutil"
)

func testPath(t *testing.T, name string) abspath.AbsolutePathBuf {
	t.Helper()
	return mustAbs(t, filepath.Join(t.TempDir(), name))
}

func loadedPlugin(t *testing.T, configName string, skillRoots []abspath.AbsolutePathBuf) LoadedPlugin[struct{}] {
	t.Helper()
	return LoadedPlugin[struct{}]{
		ConfigName:         configName,
		Root:               mustAbs(t, filepath.Join("/tmp/plugin-roots", configName)),
		Enabled:            true,
		SkillRoots:         skillRoots,
		DisabledSkillPaths: map[abspath.AbsolutePathBuf]struct{}{},
		HasEnabledSkills:   true,
		McpServers:         map[string]struct{}{},
	}
}

func TestEffectivePluginSkillRootsPreservesFirstPluginForSharedRoot(t *testing.T) {
	shared := testPath(t, "shared-skills")
	outcome := PluginLoadOutcomeFromPlugins([]LoadedPlugin[struct{}]{
		loadedPlugin(t, "zeta@test", []abspath.AbsolutePathBuf{shared}),
		loadedPlugin(t, "alpha@test", []abspath.AbsolutePathBuf{shared}),
	})

	got := outcome.EffectivePluginSkillRoots()
	want := []pluginutil.PluginSkillRoot{{
		Path:       shared,
		PluginID:   "zeta@test",
		PluginRoot: mustAbs(t, filepath.Join("/tmp/plugin-roots", "zeta@test")),
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestEffectiveSkillRootsSortedDeduped(t *testing.T) {
	a := mustAbs(t, "/tmp/a")
	b := mustAbs(t, "/tmp/b")
	outcome := PluginLoadOutcomeFromPlugins([]LoadedPlugin[struct{}]{
		loadedPlugin(t, "p2@test", []abspath.AbsolutePathBuf{b, a}),
		loadedPlugin(t, "p1@test", []abspath.AbsolutePathBuf{a}),
	})
	got := outcome.EffectiveSkillRoots()
	want := []abspath.AbsolutePathBuf{a, b}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCapabilitySummariesOnlyForActiveCapablePlugins(t *testing.T) {
	withMcp := loadedPlugin(t, "mcp@test", nil)
	withMcp.HasEnabledSkills = false
	withMcp.McpServers = map[string]struct{}{"server-b": {}, "server-a": {}}

	noCap := loadedPlugin(t, "empty@test", nil)
	noCap.HasEnabledSkills = false
	noCap.McpServers = map[string]struct{}{}

	disabled := loadedPlugin(t, "disabled@test", nil)
	disabled.Enabled = false

	errored := loadedPlugin(t, "err@test", nil)
	msg := "boom"
	errored.Error = &msg

	outcome := PluginLoadOutcomeFromPlugins([]LoadedPlugin[struct{}]{withMcp, noCap, disabled, errored})
	summaries := outcome.CapabilitySummaries()
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d (%+v)", len(summaries), summaries)
	}
	s := summaries[0]
	if s.ConfigName != "mcp@test" {
		t.Fatalf("config name got %q", s.ConfigName)
	}
	if !reflect.DeepEqual(s.McpServerNames, []string{"server-a", "server-b"}) {
		t.Fatalf("mcp names got %v (expected sorted)", s.McpServerNames)
	}
	if s.DisplayName != "mcp@test" {
		t.Fatalf("display name got %q", s.DisplayName)
	}
}

func TestPromptSafePluginDescription(t *testing.T) {
	if got := PromptSafePluginDescription(nil); got != nil {
		t.Fatalf("nil input should yield nil, got %v", *got)
	}
	empty := "   "
	if got := PromptSafePluginDescription(&empty); got != nil {
		t.Fatalf("empty input should yield nil, got %v", *got)
	}
	multi := "  hello   world  \n  foo "
	got := PromptSafePluginDescription(&multi)
	if got == nil || *got != "hello world foo" {
		t.Fatalf("got %v", got)
	}
}

func TestEffectiveMcpServersFirstWins(t *testing.T) {
	type cfg struct{ V int }
	p1 := LoadedPlugin[cfg]{ConfigName: "p1@t", Enabled: true, McpServers: map[string]cfg{"s": {V: 1}}}
	p2 := LoadedPlugin[cfg]{ConfigName: "p2@t", Enabled: true, McpServers: map[string]cfg{"s": {V: 2}}}
	outcome := PluginLoadOutcomeFromPlugins([]LoadedPlugin[cfg]{p1, p2})
	got := outcome.EffectiveMcpServers()
	if got["s"].V != 1 {
		t.Fatalf("expected first definition to win, got %d", got["s"].V)
	}
}

func TestEffectiveAppsDeduped(t *testing.T) {
	p1 := LoadedPlugin[struct{}]{ConfigName: "p1@t", Enabled: true, Apps: []AppConnectorID{{ID: "x"}, {ID: "y"}}}
	p2 := LoadedPlugin[struct{}]{ConfigName: "p2@t", Enabled: true, Apps: []AppConnectorID{{ID: "y"}, {ID: "z"}}}
	outcome := PluginLoadOutcomeFromPlugins([]LoadedPlugin[struct{}]{p1, p2})
	got := outcome.EffectiveApps()
	want := []AppConnectorID{{ID: "x"}, {ID: "y"}, {ID: "z"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
