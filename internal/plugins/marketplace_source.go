package plugins

// Marketplace plugin source resolution: local path validation and git URL
// normalization. Ports the source-resolution helpers of
// `codex-rs/core-plugins/src/marketplace.rs`.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// rawMarketplacePluginSourceObject mirrors the serde tagged enum
// `RawMarketplaceManifestPluginSourceObject` (tag = "source", lowercase). The
// "git-subdir" variant requires a path.
type rawMarketplacePluginSourceObject struct {
	Source  string  `json:"source"`
	Path    *string `json:"path"`
	URL     *string `json:"url"`
	RefName *string `json:"ref"`
	SHA     *string `json:"sha"`
}

// resolveSupportedPluginSource mirrors the Rust `resolve_supported_plugin_source`:
// unsupported and failed-to-resolve sources are dropped (ok=false) rather than
// erroring, so a single bad entry does not abort marketplace loading.
func resolveSupportedPluginSource(
	marketplacePath abspath.AbsolutePathBuf,
	pluginName string,
	rawSource json.RawMessage,
) (MarketplacePluginSource, bool) {
	source, err := resolvePluginSource(marketplacePath, rawSource)
	if err != nil {
		return MarketplacePluginSource{}, false
	}
	return source, true
}

// resolvePluginSource mirrors the Rust `resolve_plugin_source`, handling the
// untagged source forms: a bare "./"-path string, a tagged object (local, url,
// git-subdir). Unsupported shapes produce an error that the caller drops.
func resolvePluginSource(
	marketplacePath abspath.AbsolutePathBuf,
	rawSource json.RawMessage,
) (MarketplacePluginSource, error) {
	if len(rawSource) == 0 || isJSONNull(rawSource) {
		return MarketplacePluginSource{}, &MarketplaceError{
			Kind: MarketplaceErrorInvalidFile, Message: "missing plugin source"}
	}

	// Path (bare string).
	var pathString string
	if err := json.Unmarshal(rawSource, &pathString); err == nil {
		resolved, err := resolveLocalPluginSourcePath(marketplacePath, pathString)
		if err != nil {
			return MarketplacePluginSource{}, err
		}
		return MarketplacePluginSource{Kind: MarketplaceSourceLocal, Path: resolved}, nil
	}

	// Tagged object.
	if isJSONObject(rawSource) {
		var obj rawMarketplacePluginSourceObject
		if err := json.Unmarshal(rawSource, &obj); err == nil {
			return resolvePluginSourceObject(marketplacePath, obj)
		}
	}

	// Unsupported source shape.
	return MarketplacePluginSource{}, &MarketplaceError{
		Kind: MarketplaceErrorInvalidPlugin, Message: "unsupported plugin source"}
}

func resolvePluginSourceObject(
	marketplacePath abspath.AbsolutePathBuf,
	obj rawMarketplacePluginSourceObject,
) (MarketplacePluginSource, error) {
	switch obj.Source {
	case "local":
		if obj.Path == nil {
			return MarketplacePluginSource{}, &MarketplaceError{
				Kind: MarketplaceErrorInvalidPlugin, Message: "unsupported plugin source"}
		}
		resolved, err := resolveLocalPluginSourcePath(marketplacePath, *obj.Path)
		if err != nil {
			return MarketplacePluginSource{}, err
		}
		return MarketplacePluginSource{Kind: MarketplaceSourceLocal, Path: resolved}, nil
	case "url":
		if obj.URL == nil {
			return MarketplacePluginSource{}, &MarketplaceError{
				Kind: MarketplaceErrorInvalidPlugin, Message: "unsupported plugin source"}
		}
		url, err := normalizeGitPluginSourceURL(marketplacePath, *obj.URL)
		if err != nil {
			return MarketplacePluginSource{}, err
		}
		var subPath *string
		if obj.Path != nil {
			normalized, err := normalizeRemotePluginSubdir(marketplacePath, *obj.Path)
			if err != nil {
				return MarketplacePluginSource{}, err
			}
			subPath = &normalized
		}
		return MarketplacePluginSource{
			Kind:    MarketplaceSourceGit,
			URL:     url,
			SubPath: subPath,
			RefName: normalizeOptionalGitSelector(obj.RefName),
			SHA:     normalizeOptionalGitSelector(obj.SHA),
		}, nil
	case "git-subdir":
		if obj.URL == nil || obj.Path == nil {
			return MarketplacePluginSource{}, &MarketplaceError{
				Kind: MarketplaceErrorInvalidPlugin, Message: "unsupported plugin source"}
		}
		url, err := normalizeGitPluginSourceURL(marketplacePath, *obj.URL)
		if err != nil {
			return MarketplacePluginSource{}, err
		}
		normalized, err := normalizeRemotePluginSubdir(marketplacePath, *obj.Path)
		if err != nil {
			return MarketplacePluginSource{}, err
		}
		return MarketplacePluginSource{
			Kind:    MarketplaceSourceGit,
			URL:     url,
			SubPath: &normalized,
			RefName: normalizeOptionalGitSelector(obj.RefName),
			SHA:     normalizeOptionalGitSelector(obj.SHA),
		}, nil
	default:
		return MarketplacePluginSource{}, &MarketplaceError{
			Kind: MarketplaceErrorInvalidPlugin, Message: "unsupported plugin source"}
	}
}

// resolveLocalPluginSourcePath mirrors the Rust `resolve_local_plugin_source_path`.
func resolveLocalPluginSourcePath(
	marketplacePath abspath.AbsolutePathBuf,
	path string,
) (abspath.AbsolutePathBuf, error) {
	relative, ok := strings.CutPrefix(path, "./")
	if !ok {
		return abspath.AbsolutePathBuf{}, invalidMarketplaceFile(marketplacePath,
			"local plugin source path must start with `./`")
	}
	if relative == "" {
		return abspath.AbsolutePathBuf{}, invalidMarketplaceFile(marketplacePath,
			"local plugin source path must not be empty")
	}
	if !relativePathStaysWithin(relative) {
		return abspath.AbsolutePathBuf{}, invalidMarketplaceFile(marketplacePath,
			"local plugin source path must stay within the marketplace root")
	}
	root, err := marketplaceRootDir(marketplacePath)
	if err != nil {
		return abspath.AbsolutePathBuf{}, err
	}
	return root.Join(filepath.FromSlash(relative)), nil
}

// normalizeRemotePluginSubdir mirrors the Rust `normalize_remote_plugin_subdir`.
func normalizeRemotePluginSubdir(marketplacePath abspath.AbsolutePathBuf, path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	trimmed = strings.TrimPrefix(trimmed, "./")
	if trimmed == "" {
		return "", invalidMarketplaceFile(marketplacePath, "git plugin source path must not be empty")
	}
	if !relativePathStaysWithin(trimmed) {
		return "", invalidMarketplaceFile(marketplacePath,
			"git plugin source path must stay within the repository root")
	}
	return trimmed, nil
}

// relativePathStaysWithin reports whether every component is a normal component
// (no "..", absolute roots or drive prefixes), mirroring the Rust Component
// check.
func relativePathStaysWithin(path string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		switch segment {
		case "":
			// Internal empty segments arise from duplicate separators; the Rust
			// `Path::components` collapses them, so they are allowed.
			continue
		case ".", "..":
			return false
		default:
			if filepath.IsAbs(segment) || filepath.VolumeName(segment) != "" {
				return false
			}
		}
	}
	return true
}

// normalizeGitPluginSourceURL mirrors the Rust `normalize_git_plugin_source_url`.
func normalizeGitPluginSourceURL(marketplacePath abspath.AbsolutePathBuf, url string) (string, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return "", invalidMarketplaceFile(marketplacePath, "git plugin source url must not be empty")
	}
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return normalizeGitHubGitURL(url), nil
	}
	if strings.HasPrefix(url, "./") || strings.HasPrefix(url, "../") ||
		strings.HasPrefix(url, ".\\") || strings.HasPrefix(url, "..\\") {
		return normalizeRelativeGitPluginSourceURL(marketplacePath, url)
	}
	if strings.HasPrefix(url, "file://") || strings.HasPrefix(url, "/") {
		return url, nil
	}
	if strings.HasPrefix(url, "ssh://") || (strings.HasPrefix(url, "git@") && strings.Contains(url, ":")) {
		return url, nil
	}
	if normalized, ok := normalizeGitHubShorthandURL(url); ok {
		return normalized, nil
	}
	return "", invalidMarketplaceFile(marketplacePath, fmt.Sprintf("invalid git plugin source url: %s", url))
}

// normalizeRelativeGitPluginSourceURL mirrors the Rust
// `normalize_relative_git_plugin_source_url`.
func normalizeRelativeGitPluginSourceURL(marketplacePath abspath.AbsolutePathBuf, url string) (string, error) {
	root, err := marketplaceRootDir(marketplacePath)
	if err != nil {
		return "", err
	}
	normalized := root.Path()
	for _, segment := range strings.FieldsFunc(url, func(r rune) bool { return r == '/' || r == '\\' }) {
		switch segment {
		case "", ".":
			// skip
		case "..":
			return "", invalidMarketplaceFile(marketplacePath,
				"relative git plugin source url must stay within the marketplace root")
		default:
			normalized = filepath.Join(normalized, segment)
		}
	}
	return normalized, nil
}

func normalizeGitHubGitURL(url string) string {
	if strings.HasPrefix(url, "https://github.com/") && !strings.HasSuffix(url, ".git") {
		return url + ".git"
	}
	return url
}

// normalizeGitHubShorthandURL mirrors the Rust `normalize_github_shorthand_url`:
// "owner/repo" -> "https://github.com/owner/repo.git".
func normalizeGitHubShorthandURL(source string) (string, bool) {
	if !looksLikeGitHubShorthand(source) {
		return "", false
	}
	segments := strings.Split(source, "/")
	owner := segments[0]
	repo := strings.TrimSuffix(segments[1], ".git")
	if repo == "" {
		return "", false
	}
	return fmt.Sprintf("https://github.com/%s/%s.git", owner, repo), true
}

func looksLikeGitHubShorthand(source string) bool {
	segments := strings.Split(source, "/")
	if len(segments) != 2 {
		return false
	}
	return isGitHubShorthandSegment(segments[0]) && isGitHubShorthandSegment(segments[1])
}

func isGitHubShorthandSegment(segment string) bool {
	if segment == "" {
		return false
	}
	for _, ch := range segment {
		isDigit := ch >= '0' && ch <= '9'
		isAlpha := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
		if !isDigit && !isAlpha && ch != '-' && ch != '_' && ch != '.' {
			return false
		}
	}
	return true
}

func invalidMarketplaceFile(marketplacePath abspath.AbsolutePathBuf, message string) *MarketplaceError {
	return &MarketplaceError{
		Kind:    MarketplaceErrorInvalidFile,
		Message: fmt.Sprintf("invalid marketplace file `%s`: %s", marketplacePath.String(), message),
	}
}
