package plugins

// Loaded-plugin aggregation: effective skill roots, MCP servers, apps, hook
// sources and model-facing capability summaries. Ports
// `codex-rs/plugin/src/lib.rs` and `codex-rs/plugin/src/load_outcome.rs`.

import (
	"sort"
	"strings"

	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
	"github.com/sqlrush/codexgo/internal/utils/pluginutil"
)

const maxCapabilitySummaryDescriptionLen = 1024

// AppConnectorID mirrors the Rust newtype `AppConnectorId(String)`. It is a
// comparable value type.
type AppConnectorID struct {
	ID string
}

// PluginCapabilitySummary mirrors the Rust `PluginCapabilitySummary`: the
// model-facing summary of an active plugin's capabilities.
type PluginCapabilitySummary struct {
	ConfigName      string
	DisplayName     string
	Description     *string
	HasSkills       bool
	McpServerNames  []string
	AppConnectorIDs []AppConnectorID
}

// PluginHookSource mirrors the Rust `PluginHookSource`: a discovered plugin hook
// file with its source metadata.
type PluginHookSource struct {
	PluginID           PluginID
	PluginRoot         abspath.AbsolutePathBuf
	PluginDataRoot     abspath.AbsolutePathBuf
	SourcePath         abspath.AbsolutePathBuf
	SourceRelativePath string
	Hooks              config.HookEventsToml
}

// PluginTelemetryMetadata mirrors the Rust `PluginTelemetryMetadata`.
type PluginTelemetryMetadata struct {
	PluginID PluginID
	// RemotePluginID is the optional backend identifier for remote plugins.
	RemotePluginID *string
	// CapabilitySummary is the optional capability summary attached for
	// telemetry.
	CapabilitySummary *PluginCapabilitySummary
}

// PluginTelemetryMetadataFromID mirrors the Rust
// `PluginTelemetryMetadata::from_plugin_id`.
func PluginTelemetryMetadataFromID(pluginID PluginID) PluginTelemetryMetadata {
	return PluginTelemetryMetadata{PluginID: pluginID}
}

// TelemetryMetadata mirrors the Rust
// `PluginCapabilitySummary::telemetry_metadata`: it parses the config name into a
// plugin id and, on success, returns metadata carrying a copy of the summary.
func (s PluginCapabilitySummary) TelemetryMetadata() (PluginTelemetryMetadata, bool) {
	pluginID, err := ParsePluginID(s.ConfigName)
	if err != nil {
		return PluginTelemetryMetadata{}, false
	}
	summary := s
	return PluginTelemetryMetadata{PluginID: pluginID, CapabilitySummary: &summary}, true
}

// LoadedPlugin mirrors the Rust `LoadedPlugin<M>`: a plugin loaded from disk
// together with its merged capabilities. M is the MCP server config type so
// callers can plug in the concrete config without this package depending on it.
type LoadedPlugin[M any] struct {
	ConfigName          string
	ManifestName        *string
	ManifestDescription *string
	Root                abspath.AbsolutePathBuf
	Enabled             bool
	SkillRoots          []abspath.AbsolutePathBuf
	DisabledSkillPaths  map[abspath.AbsolutePathBuf]struct{}
	HasEnabledSkills    bool
	McpServers          map[string]M
	Apps                []AppConnectorID
	HookSources         []PluginHookSource
	HookLoadWarnings    []string
	Error               *string
}

// IsActive mirrors the Rust `LoadedPlugin::is_active`.
func (p LoadedPlugin[M]) IsActive() bool {
	return p.Enabled && p.Error == nil
}

// PromptSafePluginDescription mirrors the Rust `prompt_safe_plugin_description`:
// it collapses whitespace and truncates to the capability-summary limit,
// returning nil when the input is nil/empty after normalization.
func PromptSafePluginDescription(description *string) *string {
	if description == nil {
		return nil
	}
	normalized := strings.Join(strings.Fields(*description), " ")
	if normalized == "" {
		return nil
	}
	runes := []rune(normalized)
	if len(runes) > maxCapabilitySummaryDescriptionLen {
		runes = runes[:maxCapabilitySummaryDescriptionLen]
	}
	truncated := string(runes)
	return &truncated
}

// pluginCapabilitySummaryFromLoaded mirrors the Rust
// `plugin_capability_summary_from_loaded`: it returns a summary only for active
// plugins that expose at least one capability.
func pluginCapabilitySummaryFromLoaded[M any](plugin LoadedPlugin[M]) (PluginCapabilitySummary, bool) {
	if !plugin.IsActive() {
		return PluginCapabilitySummary{}, false
	}

	names := make([]string, 0, len(plugin.McpServers))
	for name := range plugin.McpServers {
		names = append(names, name)
	}
	sort.Strings(names)

	displayName := plugin.ConfigName
	if plugin.ManifestName != nil {
		displayName = *plugin.ManifestName
	}

	summary := PluginCapabilitySummary{
		ConfigName:      plugin.ConfigName,
		DisplayName:     displayName,
		Description:     PromptSafePluginDescription(plugin.ManifestDescription),
		HasSkills:       plugin.HasEnabledSkills,
		McpServerNames:  names,
		AppConnectorIDs: cloneAppConnectorIDs(plugin.Apps),
	}

	if summary.HasSkills || len(summary.McpServerNames) > 0 || len(summary.AppConnectorIDs) > 0 {
		return summary, true
	}
	return PluginCapabilitySummary{}, false
}

func cloneAppConnectorIDs(in []AppConnectorID) []AppConnectorID {
	if len(in) == 0 {
		return nil
	}
	out := make([]AppConnectorID, len(in))
	copy(out, in)
	return out
}

// PluginLoadOutcome mirrors the Rust `PluginLoadOutcome<M>`: the result of
// loading configured plugins, with precomputed capability summaries.
type PluginLoadOutcome[M any] struct {
	plugins             []LoadedPlugin[M]
	capabilitySummaries []PluginCapabilitySummary
}

// PluginLoadOutcomeFromPlugins mirrors the Rust `from_plugins`: it stores the
// plugins and precomputes capability summaries for active, capable plugins.
func PluginLoadOutcomeFromPlugins[M any](plugins []LoadedPlugin[M]) PluginLoadOutcome[M] {
	var summaries []PluginCapabilitySummary
	for _, plugin := range plugins {
		if summary, ok := pluginCapabilitySummaryFromLoaded(plugin); ok {
			summaries = append(summaries, summary)
		}
	}
	return PluginLoadOutcome[M]{plugins: plugins, capabilitySummaries: summaries}
}

// EffectiveSkillRoots mirrors the Rust `effective_skill_roots`: sorted, deduped
// skill roots of all active plugins.
func (o PluginLoadOutcome[M]) EffectiveSkillRoots() []abspath.AbsolutePathBuf {
	var roots []abspath.AbsolutePathBuf
	for _, plugin := range o.plugins {
		if !plugin.IsActive() {
			continue
		}
		roots = append(roots, plugin.SkillRoots...)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].Path() < roots[j].Path() })
	return dedupAbsolutePaths(roots)
}

// EffectivePluginSkillRoots mirrors the Rust `effective_plugin_skill_roots`: for
// each unique skill-root path, keep the first active plugin that declares it,
// sorted by path.
func (o PluginLoadOutcome[M]) EffectivePluginSkillRoots() []pluginutil.PluginSkillRoot {
	var roots []pluginutil.PluginSkillRoot
	seen := make(map[abspath.AbsolutePathBuf]struct{})
	for _, plugin := range o.plugins {
		if !plugin.IsActive() {
			continue
		}
		for _, path := range plugin.SkillRoots {
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			roots = append(roots, pluginutil.PluginSkillRoot{
				Path:       path,
				PluginID:   plugin.ConfigName,
				PluginRoot: plugin.Root,
			})
		}
	}
	sort.SliceStable(roots, func(i, j int) bool { return roots[i].Path.Path() < roots[j].Path.Path() })
	return roots
}

// EffectiveMcpServers mirrors the Rust `effective_mcp_servers`: the union of
// active plugins' MCP servers, keeping the first definition for each name.
func (o PluginLoadOutcome[M]) EffectiveMcpServers() map[string]M {
	servers := make(map[string]M)
	for _, plugin := range o.plugins {
		if !plugin.IsActive() {
			continue
		}
		for name, cfg := range plugin.McpServers {
			if _, ok := servers[name]; !ok {
				servers[name] = cfg
			}
		}
	}
	return servers
}

// EffectiveApps mirrors the Rust `effective_apps`: deduped app connector ids of
// active plugins, in first-seen order.
func (o PluginLoadOutcome[M]) EffectiveApps() []AppConnectorID {
	var apps []AppConnectorID
	seen := make(map[AppConnectorID]struct{})
	for _, plugin := range o.plugins {
		if !plugin.IsActive() {
			continue
		}
		for _, connectorID := range plugin.Apps {
			if _, ok := seen[connectorID]; ok {
				continue
			}
			seen[connectorID] = struct{}{}
			apps = append(apps, connectorID)
		}
	}
	return apps
}

// EffectivePluginHookSources mirrors the Rust `effective_plugin_hook_sources`.
func (o PluginLoadOutcome[M]) EffectivePluginHookSources() []PluginHookSource {
	var sources []PluginHookSource
	for _, plugin := range o.plugins {
		if !plugin.IsActive() {
			continue
		}
		sources = append(sources, plugin.HookSources...)
	}
	return sources
}

// EffectivePluginHookWarnings mirrors the Rust `effective_plugin_hook_warnings`.
func (o PluginLoadOutcome[M]) EffectivePluginHookWarnings() []string {
	var warnings []string
	for _, plugin := range o.plugins {
		if !plugin.IsActive() {
			continue
		}
		warnings = append(warnings, plugin.HookLoadWarnings...)
	}
	return warnings
}

// CapabilitySummaries mirrors the Rust `capability_summaries`.
func (o PluginLoadOutcome[M]) CapabilitySummaries() []PluginCapabilitySummary {
	return o.capabilitySummaries
}

// Plugins mirrors the Rust `plugins`.
func (o PluginLoadOutcome[M]) Plugins() []LoadedPlugin[M] {
	return o.plugins
}

// dedupAbsolutePaths removes consecutive duplicate paths from a sorted slice,
// mirroring Rust's `Vec::dedup`.
func dedupAbsolutePaths(paths []abspath.AbsolutePathBuf) []abspath.AbsolutePathBuf {
	if len(paths) == 0 {
		return nil
	}
	out := paths[:1]
	for _, path := range paths[1:] {
		if path != out[len(out)-1] {
			out = append(out, path)
		}
	}
	return out
}
