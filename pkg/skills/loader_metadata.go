package skills

import (
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// resolveInterface converts a parsed sidecar `interface` mapping into a
// SkillInterface, validating string lengths and icon asset paths. It mirrors the
// Rust `resolve_interface`, returning nil when no field survives validation.
func resolveInterface(node yamlValue, skillDir abspath.AbsolutePathBuf, pluginRoot *abspath.AbsolutePathBuf) *SkillInterface {
	iface := &SkillInterface{
		DisplayName:      resolveStr(node, "display_name", maxNameLen),
		ShortDescription: resolveStr(node, "short_description", maxShortDescriptionLen),
		IconSmall:        resolveAssetPath(node, "icon_small", skillDir, pluginRoot),
		IconLarge:        resolveAssetPath(node, "icon_large", skillDir, pluginRoot),
		BrandColor:       resolveColorStr(node, "brand_color"),
		DefaultPrompt:    resolveStr(node, "default_prompt", maxDefaultPromptLen),
	}
	if iface.DisplayName == nil && iface.ShortDescription == nil && iface.IconSmall == nil &&
		iface.IconLarge == nil && iface.BrandColor == nil && iface.DefaultPrompt == nil {
		return nil
	}
	return iface
}

// resolveDependencies converts a parsed sidecar `dependencies` mapping into a
// SkillDependencies, dropping malformed tool entries. It mirrors the Rust
// `resolve_dependencies`, returning nil when no tool survives.
func resolveDependencies(node yamlValue) *SkillDependencies {
	toolsValue, ok := node.get("tools")
	if !ok || toolsValue.kind != yamlSequence {
		return nil
	}
	var tools []SkillToolDependency
	for _, toolNode := range toolsValue.sequence {
		if toolNode.kind != yamlMapping {
			continue
		}
		if tool, ok := resolveDependencyTool(toolNode); ok {
			tools = append(tools, tool)
		}
	}
	if len(tools) == 0 {
		return nil
	}
	return &SkillDependencies{Tools: tools}
}

// resolvePolicy converts a parsed sidecar `policy` mapping into a SkillPolicy. It
// mirrors the Rust `resolve_policy`.
func resolvePolicy(node yamlValue) *SkillPolicy {
	policy := &SkillPolicy{}
	if allow, ok := node.get("allow_implicit_invocation"); ok {
		if b, ok := allow.asBool(); ok {
			policy.AllowImplicitInvocation = &b
		}
	}
	if products, ok := node.get("products"); ok && products.kind == yamlSequence {
		for _, item := range products.sequence {
			if s, ok := item.asString(); ok {
				if product, ok := parseProduct(s); ok {
					policy.Products = append(policy.Products, product)
				}
			}
		}
	}
	return policy
}

// parseProduct parses a Product value, accepting both lowercase and the
// uppercase serde aliases (CHATGPT/CODEX/ATLAS).
func parseProduct(value string) (Product, bool) {
	switch value {
	case "chatgpt", "CHATGPT":
		return ProductChatgpt, true
	case "codex", "CODEX":
		return ProductCodex, true
	case "atlas", "ATLAS":
		return ProductAtlas, true
	default:
		return "", false
	}
}

// resolveDependencyTool converts a parsed tool mapping into a
// SkillToolDependency, requiring type and value. It mirrors the Rust
// `resolve_dependency_tool`.
func resolveDependencyTool(node yamlValue) (SkillToolDependency, bool) {
	kind := resolveRequiredStr(node, "type", maxDependencyTypeLen)
	if kind == nil {
		return SkillToolDependency{}, false
	}
	value := resolveRequiredStr(node, "value", maxDependencyValueLen)
	if value == nil {
		return SkillToolDependency{}, false
	}
	return SkillToolDependency{
		Type:        *kind,
		Value:       *value,
		Description: resolveStr(node, "description", maxDependencyDescLen),
		Transport:   resolveStr(node, "transport", maxDependencyTranspLen),
		Command:     resolveStr(node, "command", maxDependencyCommandLen),
		URL:         resolveStr(node, "url", maxDependencyURLLen),
	}, true
}

// resolveStr sanitizes and length-validates an optional string field, mirroring
// the Rust `resolve_str` (returns nil for empty or over-length values).
func resolveStr(node yamlValue, field string, maxLen int) *string {
	value, ok := node.get(field)
	if !ok {
		return nil
	}
	raw, ok := value.asString()
	if !ok {
		return nil
	}
	sanitized := sanitizeSingleLine(raw)
	if sanitized == "" {
		return nil
	}
	if utf8.RuneCountInString(sanitized) > maxLen {
		return nil
	}
	return &sanitized
}

// resolveRequiredStr is resolveStr for fields the caller treats as required; it
// returns nil when the field is missing or invalid, mirroring the Rust
// `resolve_required_str`.
func resolveRequiredStr(node yamlValue, field string, maxLen int) *string {
	return resolveStr(node, field, maxLen)
}

// resolveColorStr validates a brand color as a #RRGGBB hex string, mirroring the
// Rust `resolve_color_str`.
func resolveColorStr(node yamlValue, field string) *string {
	value, ok := node.get(field)
	if !ok {
		return nil
	}
	raw, ok := value.asString()
	if !ok {
		return nil
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	if len(trimmed) == 7 && trimmed[0] == '#' && isAllHexDigits(trimmed[1:]) {
		out := trimmed
		return &out
	}
	return nil
}

func isAllHexDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// resolveAssetPath validates an icon path. Icons must be relative paths under the
// skill's assets/ directory; plugin skills may reference the plugin's shared
// assets/ directory via "..". It mirrors the Rust `resolve_asset_path`.
func resolveAssetPath(node yamlValue, field string, skillDir abspath.AbsolutePathBuf, pluginRoot *abspath.AbsolutePathBuf) *abspath.AbsolutePathBuf {
	value, ok := node.get(field)
	if !ok {
		return nil
	}
	raw, ok := value.asString()
	if !ok {
		return nil
	}
	if raw == "" {
		return nil
	}
	if filepath.IsAbs(raw) {
		return nil
	}

	// Walk path components: reject anything other than "assets/<...>" unless a
	// ".." appears, in which case fall back to the plugin shared-asset resolver.
	var normalized []string
	for _, comp := range splitPathComponents(raw) {
		switch comp {
		case ".", "":
			continue
		case "..":
			return resolvePluginSharedAssetPath(skillDir, pluginRoot, raw)
		default:
			normalized = append(normalized, comp)
		}
	}
	if len(normalized) == 0 || normalized[0] != "assets" {
		return nil
	}
	resolved := skillDir.Join(strings.Join(normalized, "/"))
	return &resolved
}

// resolvePluginSharedAssetPath resolves an icon path containing ".." against the
// plugin's assets/ directory, mirroring the Rust
// `resolve_plugin_shared_asset_path`.
func resolvePluginSharedAssetPath(skillDir abspath.AbsolutePathBuf, pluginRoot *abspath.AbsolutePathBuf, raw string) *abspath.AbsolutePathBuf {
	if pluginRoot == nil {
		return nil
	}
	pluginAssetsDir := pluginRoot.Join("assets")
	resolved := skillDir.Join(raw)
	if !pathStartsWith(resolved, pluginAssetsDir) {
		return nil
	}
	out := resolved
	return &out
}

// splitPathComponents splits a relative path into components using both "/" and
// the OS separator, mirroring Rust's component iteration on PathBuf.
func splitPathComponents(path string) []string {
	normalized := strings.ReplaceAll(path, "\\", "/")
	return strings.Split(normalized, "/")
}
