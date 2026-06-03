package plugins

// On-disk plugin manifest schema and loader. Ports
// `codex-rs/core-plugins/src/manifest.rs`.
//
// The manifest lives at ".codex-plugin/plugin.json" with a ".claude-plugin/"
// fallback. All manifest paths are required to be "./"-relative to the plugin
// root and are normalized to absolute paths before being returned.

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
	"github.com/sqlrush/codexgo/internal/utils/pluginutil"
)

const (
	maxDefaultPromptCount = 3
	maxDefaultPromptLen   = 128
)

// PluginManifest mirrors the Rust `PluginManifest`: the resolved, validated
// manifest of a plugin. It is an immutable value; copies are independent except
// for shared slices, which callers must not mutate.
type PluginManifest struct {
	Name        string
	Version     *string
	Description *string
	Keywords    []string
	Paths       PluginManifestPaths
	Interface   *PluginManifestInterface
}

// PluginManifestPaths mirrors the Rust `PluginManifestPaths`: the resolved
// absolute paths declared in the manifest. A nil pointer means the field was
// absent or invalid.
type PluginManifestPaths struct {
	Skills     *abspath.AbsolutePathBuf
	McpServers *abspath.AbsolutePathBuf
	Apps       *abspath.AbsolutePathBuf
	Hooks      *PluginManifestHooks
}

// PluginManifestHooksKind selects which variant of [PluginManifestHooks] is set.
type PluginManifestHooksKind int

const (
	// PluginManifestHooksPaths indicates the Paths field is populated.
	PluginManifestHooksPaths PluginManifestHooksKind = iota
	// PluginManifestHooksInline indicates the Inline field is populated.
	PluginManifestHooksInline
)

// PluginManifestHooks mirrors the Rust enum `PluginManifestHooks`. Exactly one
// variant is active, selected by Kind.
type PluginManifestHooks struct {
	Kind   PluginManifestHooksKind
	Paths  []abspath.AbsolutePathBuf
	Inline []config.HooksFile
}

// PluginManifestInterface mirrors the Rust `PluginManifestInterface`: the
// optional display/branding metadata declared under the manifest "interface"
// key.
type PluginManifestInterface struct {
	DisplayName       *string
	ShortDescription  *string
	LongDescription   *string
	DeveloperName     *string
	Category          *string
	Capabilities      []string
	WebsiteURL        *string
	PrivacyPolicyURL  *string
	TermsOfServiceURL *string
	DefaultPrompt     *[]string
	BrandColor        *string
	ComposerIcon      *abspath.AbsolutePathBuf
	Logo              *abspath.AbsolutePathBuf
	Screenshots       []abspath.AbsolutePathBuf
}

// rawPluginManifest mirrors the serde `RawPluginManifest`. JSON keys are
// camelCase. Path-bearing fields stay as raw strings so the required "./" syntax
// is validated before resolution. Unknown fields are ignored.
type rawPluginManifest struct {
	Name        string                      `json:"name"`
	Version     *string                     `json:"version"`
	Description *string                     `json:"description"`
	Keywords    []string                    `json:"keywords"`
	Skills      *string                     `json:"skills"`
	McpServers  *string                     `json:"mcpServers"`
	Apps        *string                     `json:"apps"`
	Hooks       json.RawMessage             `json:"hooks"`
	Interface   *rawPluginManifestInterface `json:"interface"`
}

// rawPluginManifestInterface mirrors the serde `RawPluginManifestInterface`,
// including the URL aliases (websiteURL etc.).
type rawPluginManifestInterface struct {
	DisplayName      *string         `json:"displayName"`
	ShortDescription *string         `json:"shortDescription"`
	LongDescription  *string         `json:"longDescription"`
	DeveloperName    *string         `json:"developerName"`
	Category         *string         `json:"category"`
	Capabilities     []string        `json:"capabilities"`
	WebsiteURL       *string         `json:"websiteUrl"`
	WebsiteURLAlias  *string         `json:"websiteURL"`
	PrivacyPolicyURL *string         `json:"privacyPolicyUrl"`
	PrivacyURLAlias  *string         `json:"privacyPolicyURL"`
	TermsURL         *string         `json:"termsOfServiceUrl"`
	TermsURLAlias    *string         `json:"termsOfServiceURL"`
	DefaultPrompt    json.RawMessage `json:"defaultPrompt"`
	BrandColor       *string         `json:"brandColor"`
	ComposerIcon     *string         `json:"composerIcon"`
	Logo             *string         `json:"logo"`
	Screenshots      []string        `json:"screenshots"`
}

// LoadPluginManifest mirrors the Rust `load_plugin_manifest`.
//
// It discovers the manifest under pluginRoot, parses it, resolves all
// "./"-relative paths to absolute paths, and returns the resolved manifest, or
// ok=false when no manifest exists or the file is not valid JSON. Invalid
// individual fields (bad paths, malformed prompts) are dropped rather than
// failing the whole load, matching the Rust behavior. pluginRoot is treated as
// a plain filesystem path and is not modified.
func LoadPluginManifest(pluginRoot string) (PluginManifest, bool) {
	manifestPath, ok := pluginutil.FindPluginManifestPath(pluginRoot)
	if !ok {
		return PluginManifest{}, false
	}
	contents, err := readFileString(manifestPath)
	if err != nil {
		return PluginManifest{}, false
	}
	var raw rawPluginManifest
	if err := json.Unmarshal([]byte(contents), &raw); err != nil {
		return PluginManifest{}, false
	}

	name := resolveManifestName(pluginRoot, raw.Name)
	version := trimmedNonEmptyOption(raw.Version)

	manifest := PluginManifest{
		Name:        name,
		Version:     version,
		Description: raw.Description,
		Keywords:    raw.Keywords,
		Paths: PluginManifestPaths{
			Skills:     resolveManifestPath(pluginRoot, "skills", raw.Skills),
			McpServers: resolveManifestPath(pluginRoot, "mcpServers", raw.McpServers),
			Apps:       resolveManifestPath(pluginRoot, "apps", raw.Apps),
			Hooks:      resolveManifestHooks(pluginRoot, raw.Hooks),
		},
		Interface: resolveManifestInterface(pluginRoot, raw.Interface),
	}
	return manifest, true
}

// resolveManifestName mirrors the Rust name fallback: when the raw name is blank
// (whitespace-only), the plugin root's final path component is used; otherwise
// the raw (untrimmed) name is used verbatim.
func resolveManifestName(pluginRoot, rawName string) string {
	if strings.TrimSpace(rawName) == "" {
		if base := finalManifestComponent(pluginRoot); base != "" {
			return base
		}
	}
	return rawName
}

func finalManifestComponent(pluginRoot string) string {
	base := filepath.Base(pluginRoot)
	switch base {
	case ".", string(filepath.Separator), "":
		return ""
	}
	if filepath.VolumeName(base) == strings.TrimRight(base, `\/`) && strings.TrimRight(base, `\/`) != "" {
		return ""
	}
	return base
}

func trimmedNonEmptyOption(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func resolveManifestInterface(pluginRoot string, raw *rawPluginManifestInterface) *PluginManifestInterface {
	if raw == nil {
		return nil
	}
	iface := PluginManifestInterface{
		DisplayName:       raw.DisplayName,
		ShortDescription:  raw.ShortDescription,
		LongDescription:   raw.LongDescription,
		DeveloperName:     raw.DeveloperName,
		Category:          raw.Category,
		Capabilities:      raw.Capabilities,
		WebsiteURL:        firstNonNil(raw.WebsiteURL, raw.WebsiteURLAlias),
		PrivacyPolicyURL:  firstNonNil(raw.PrivacyPolicyURL, raw.PrivacyURLAlias),
		TermsOfServiceURL: firstNonNil(raw.TermsURL, raw.TermsURLAlias),
		DefaultPrompt:     resolveDefaultPrompts(pluginRoot, raw.DefaultPrompt),
		BrandColor:        raw.BrandColor,
		ComposerIcon:      resolveManifestPath(pluginRoot, "interface.composerIcon", raw.ComposerIcon),
		Logo:              resolveManifestPath(pluginRoot, "interface.logo", raw.Logo),
		Screenshots:       resolveScreenshots(pluginRoot, raw.Screenshots),
	}

	if interfaceHasFields(iface) {
		return &iface
	}
	return nil
}

// firstNonNil mirrors serde's `#[serde(alias = ...)]`: the canonical key is
// preferred; the alias is used only when the canonical key is absent.
func firstNonNil(primary, alias *string) *string {
	if primary != nil {
		return primary
	}
	return alias
}

func resolveScreenshots(pluginRoot string, screenshots []string) []abspath.AbsolutePathBuf {
	var resolved []abspath.AbsolutePathBuf
	for i := range screenshots {
		shot := screenshots[i]
		if path := resolveManifestPath(pluginRoot, "interface.screenshots", &shot); path != nil {
			resolved = append(resolved, *path)
		}
	}
	return resolved
}

func interfaceHasFields(iface PluginManifestInterface) bool {
	return iface.DisplayName != nil ||
		iface.ShortDescription != nil ||
		iface.LongDescription != nil ||
		iface.DeveloperName != nil ||
		iface.Category != nil ||
		len(iface.Capabilities) > 0 ||
		iface.WebsiteURL != nil ||
		iface.PrivacyPolicyURL != nil ||
		iface.TermsOfServiceURL != nil ||
		iface.DefaultPrompt != nil ||
		iface.BrandColor != nil ||
		iface.ComposerIcon != nil ||
		iface.Logo != nil ||
		len(iface.Screenshots) > 0
}

// resolveDefaultPrompts mirrors the Rust `resolve_default_prompts` untagged enum
// handling: a single string, an array of strings (max 3, invalid entries
// dropped), or anything else (ignored).
func resolveDefaultPrompts(pluginRoot string, raw json.RawMessage) *[]string {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		if prompt, ok := resolveDefaultPromptStr(asString); ok {
			return &[]string{prompt}
		}
		return nil
	}

	var asList []json.RawMessage
	if err := json.Unmarshal(raw, &asList); err == nil {
		prompts := make([]string, 0, len(asList))
		for _, item := range asList {
			if len(prompts) >= maxDefaultPromptCount {
				break
			}
			var entry string
			if err := json.Unmarshal(item, &entry); err != nil {
				continue
			}
			if prompt, ok := resolveDefaultPromptStr(entry); ok {
				prompts = append(prompts, prompt)
			}
		}
		if len(prompts) == 0 {
			return nil
		}
		return &prompts
	}

	return nil
}

func resolveDefaultPromptStr(prompt string) (string, bool) {
	normalized := strings.Join(strings.Fields(prompt), " ")
	if normalized == "" {
		return "", false
	}
	if len([]rune(normalized)) > maxDefaultPromptLen {
		return "", false
	}
	return normalized, true
}

// resolveManifestHooks mirrors the Rust `resolve_manifest_hooks` untagged enum:
// a single "./"-path string, an array of path strings, an inline HooksFile
// object, or an array of inline HooksFile objects. Anything else is ignored.
func resolveManifestHooks(pluginRoot string, raw json.RawMessage) *PluginManifestHooks {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil
	}

	// Path (single string).
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		if path := resolveManifestPath(pluginRoot, "hooks", &single); path != nil {
			return &PluginManifestHooks{Kind: PluginManifestHooksPaths, Paths: []abspath.AbsolutePathBuf{*path}}
		}
		return nil
	}

	// Inline (single object) takes priority over array-of-strings when the JSON
	// is an object; array forms are handled below.
	if isJSONObject(raw) {
		var hooksFile config.HooksFile
		if err := json.Unmarshal(raw, &hooksFile); err == nil {
			return &PluginManifestHooks{Kind: PluginManifestHooksInline, Inline: []config.HooksFile{hooksFile}}
		}
		return nil
	}

	if isJSONArray(raw) {
		// Paths (array of strings).
		var paths []string
		if err := json.Unmarshal(raw, &paths); err == nil {
			resolved := make([]abspath.AbsolutePathBuf, 0, len(paths))
			for i := range paths {
				p := paths[i]
				if path := resolveManifestPath(pluginRoot, "hooks", &p); path != nil {
					resolved = append(resolved, *path)
				}
			}
			if len(resolved) == 0 {
				return nil
			}
			return &PluginManifestHooks{Kind: PluginManifestHooksPaths, Paths: resolved}
		}
		// InlineList (array of objects).
		var hooksFiles []config.HooksFile
		if err := json.Unmarshal(raw, &hooksFiles); err == nil {
			if len(hooksFiles) == 0 {
				return nil
			}
			return &PluginManifestHooks{Kind: PluginManifestHooksInline, Inline: hooksFiles}
		}
	}

	return nil
}

// resolveManifestPath mirrors the Rust `resolve_manifest_path`.
//
// Manifest paths must be "./"-relative to the plugin root, must not be "./",
// must not contain ".." or absolute/prefix components, and resolve to an
// absolute path under the plugin root. Invalid paths return nil (the Rust
// `None`).
func resolveManifestPath(pluginRoot, _field string, path *string) *abspath.AbsolutePathBuf {
	if path == nil {
		return nil
	}
	value := *path
	if value == "" {
		return nil
	}
	relativePath, ok := strings.CutPrefix(value, "./")
	if !ok {
		return nil
	}
	if relativePath == "" {
		return nil
	}

	normalized, ok := normalizeRelativeManifestPath(relativePath)
	if !ok {
		return nil
	}

	resolved, err := abspath.FromAbsolutePathChecked(filepath.Join(pluginRoot, normalized))
	if err != nil {
		return nil
	}
	return &resolved
}

// normalizeRelativeManifestPath walks a "./"-stripped relative path and rejects
// any non-normal component (.., absolute roots, drive prefixes), mirroring the
// Rust component match. The returned path uses the OS separator.
func normalizeRelativeManifestPath(relativePath string) (string, bool) {
	cleaned := filepath.ToSlash(relativePath)
	segments := strings.Split(cleaned, "/")
	out := make([]string, 0, len(segments))
	for _, segment := range segments {
		switch segment {
		case "", ".":
			// CurDir components are skipped; empty segments arise from
			// duplicate separators and carry no path meaning.
			continue
		case "..":
			return "", false
		default:
			// Reject embedded volume names / absolute markers.
			if filepath.IsAbs(segment) || filepath.VolumeName(segment) != "" {
				return "", false
			}
			out = append(out, segment)
		}
	}
	if len(out) == 0 {
		return "", false
	}
	return filepath.Join(out...), true
}

func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

func isJSONObject(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return strings.HasPrefix(trimmed, "{")
}

func isJSONArray(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return strings.HasPrefix(trimmed, "[")
}
