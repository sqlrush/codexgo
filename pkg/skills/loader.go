package skills

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
	"github.com/sqlrush/codexgo/internal/utils/pluginutil"
)

const (
	skillsFilename          = "SKILL.md"
	skillsMetadataDir       = "agents"
	skillsMetadataFilename  = "openai.yaml"
	maxNameLen              = 64
	maxDescriptionLen       = 1024
	maxShortDescriptionLen  = maxDescriptionLen
	maxDefaultPromptLen     = maxDescriptionLen
	maxDependencyTypeLen    = maxNameLen
	maxDependencyTranspLen  = maxNameLen
	maxDependencyValueLen   = maxDescriptionLen
	maxDependencyDescLen    = maxDescriptionLen
	maxDependencyCommandLen = maxDescriptionLen
	maxDependencyURLLen     = maxDescriptionLen
	// maxScanDepth bounds traversal depth from the skills root.
	maxScanDepth = 6
	// maxSkillsDirsPerRoot bounds the number of directories scanned per root.
	maxSkillsDirsPerRoot = 2000
)

// SkillRoot describes a directory to scan for skills together with the scope and
// filesystem used to read it. It mirrors the Rust `SkillRoot`.
type SkillRoot struct {
	// Path is the directory to scan (e.g. ".codex/skills" or a plugin's skills dir).
	Path abspath.AbsolutePathBuf
	// Scope classifies the skills discovered under this root.
	Scope SkillScope
	// FileSystem reads this root; nil falls back to the local filesystem.
	FileSystem FileSystem
	// PluginID, when set, namespaces and tags skills under this root.
	PluginID *string
	// PluginRoot is the owning plugin's root (used to resolve shared icon assets).
	PluginRoot *abspath.AbsolutePathBuf
}

// skillParseError categorizes a SKILL.md parse failure. Its Error string mirrors
// the Rust `SkillParseError` Display output so SkillError messages match.
type skillParseError struct {
	kind   parseErrorKind
	field  string
	reason string
	cause  error
}

type parseErrorKind int

const (
	parseErrRead parseErrorKind = iota
	parseErrMissingFrontmatter
	parseErrInvalidYaml
	parseErrMissingField
	parseErrInvalidField
)

func (e *skillParseError) Error() string {
	switch e.kind {
	case parseErrRead:
		return fmt.Sprintf("failed to read file: %v", e.cause)
	case parseErrMissingFrontmatter:
		return "missing YAML frontmatter delimited by ---"
	case parseErrInvalidYaml:
		return fmt.Sprintf("invalid YAML: %v", e.cause)
	case parseErrMissingField:
		return fmt.Sprintf("missing field `%s`", e.field)
	case parseErrInvalidField:
		return fmt.Sprintf("invalid %s: %s", e.field, e.reason)
	default:
		return "unknown skill parse error"
	}
}

func (e *skillParseError) Unwrap() error { return e.cause }

// LoadSkillsFromRoots discovers, parses, deduplicates, and sorts skills from the
// given roots. It mirrors the Rust `load_skills_from_roots`: roots are scanned
// in order, the first occurrence of a SKILL.md path wins on dedupe, and skills
// are sorted by scope rank, then name, then path.
//
// A nil FileSystem on a root falls back to [LocalFS]. The returned outcome's
// unexported indexes (skill roots, per-skill filesystems) are populated for
// later rendering; DisabledPaths and implicit indexes are populated by the
// manager.
func LoadSkillsFromRoots(ctx context.Context, roots []SkillRoot) SkillLoadOutcome {
	var outcome SkillLoadOutcome
	var skillRoots []abspath.AbsolutePathBuf
	skillRootByPath := make(map[abspath.AbsolutePathBuf]abspath.AbsolutePathBuf)
	fileSystemsBySkillPath := make(map[abspath.AbsolutePathBuf]FileSystem)

	for _, root := range roots {
		fs := root.FileSystem
		if fs == nil {
			fs = LocalFS{}
		}
		rootPath := canonicalizeForSkillIdentity(root.Path)
		before := len(outcome.Skills)
		discoverSkillsUnderRoot(ctx, fs, rootPath, root.Scope, root.PluginID, root.PluginRoot, &outcome)
		for i := before; i < len(outcome.Skills); i++ {
			skill := outcome.Skills[i]
			if !containsPath(skillRoots, rootPath) {
				skillRoots = append(skillRoots, rootPath)
			}
			if _, ok := skillRootByPath[skill.PathToSkillsMd]; !ok {
				skillRootByPath[skill.PathToSkillsMd] = rootPath
			}
			if _, ok := fileSystemsBySkillPath[skill.PathToSkillsMd]; !ok {
				fileSystemsBySkillPath[skill.PathToSkillsMd] = fs
			}
		}
	}

	// Dedupe by SKILL.md path, preferring the first root that discovered it.
	seen := make(map[abspath.AbsolutePathBuf]struct{})
	deduped := outcome.Skills[:0]
	for _, skill := range outcome.Skills {
		if _, ok := seen[skill.PathToSkillsMd]; ok {
			continue
		}
		seen[skill.PathToSkillsMd] = struct{}{}
		deduped = append(deduped, skill)
	}
	outcome.Skills = deduped

	retained := make(map[abspath.AbsolutePathBuf]struct{}, len(outcome.Skills))
	for _, skill := range outcome.Skills {
		retained[skill.PathToSkillsMd] = struct{}{}
	}
	for path := range skillRootByPath {
		if _, ok := retained[path]; !ok {
			delete(skillRootByPath, path)
		}
	}
	usedRoots := make(map[abspath.AbsolutePathBuf]struct{})
	for _, root := range skillRootByPath {
		usedRoots[root] = struct{}{}
	}
	skillRoots = retainRoots(skillRoots, usedRoots)
	for path := range fileSystemsBySkillPath {
		if _, ok := retained[path]; !ok {
			delete(fileSystemsBySkillPath, path)
		}
	}
	outcome.skillRoots = skillRoots
	outcome.skillRootByPath = skillRootByPath
	outcome.fileSystemsBySkillPath = fileSystemsBySkillPath

	sort.SliceStable(outcome.Skills, func(i, j int) bool {
		a, b := outcome.Skills[i], outcome.Skills[j]
		if ra, rb := scopeRank(a.Scope), scopeRank(b.Scope); ra != rb {
			return ra < rb
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return comparePaths(a.PathToSkillsMd, b.PathToSkillsMd) < 0
	})

	return outcome
}

func containsPath(paths []abspath.AbsolutePathBuf, target abspath.AbsolutePathBuf) bool {
	for _, p := range paths {
		if p == target {
			return true
		}
	}
	return false
}

// dirQueueItem is a (path, depth) pair in the BFS traversal queue.
type dirQueueItem struct {
	path  abspath.AbsolutePathBuf
	depth int
}

// discoverSkillsUnderRoot walks root breadth-first, parsing every SKILL.md it
// finds. It mirrors the Rust `discover_skills_under_root`, including symlink
// following rules, depth and directory-count limits, and the rule that hidden
// entries (starting with ".") are skipped.
func discoverSkillsUnderRoot(
	ctx context.Context,
	fs FileSystem,
	root abspath.AbsolutePathBuf,
	scope SkillScope,
	pluginID *string,
	pluginRoot *abspath.AbsolutePathBuf,
	outcome *SkillLoadOutcome,
) {
	root = canonicalizeForSkillIdentity(root)
	var resolvedPluginRoot *abspath.AbsolutePathBuf
	if pluginRoot != nil {
		c := canonicalizeForSkillIdentity(*pluginRoot)
		resolvedPluginRoot = &c
	}

	metadata, err := fs.GetMetadata(ctx, root)
	if err != nil {
		if isNotExist(err) {
			return
		}
		return
	}
	if !metadata.IsDirectory {
		return
	}

	// Follow symlinked directories for user, admin, and repo skills. System
	// skills are written by Codex itself.
	followSymlinks := scope == SkillScopeRepo || scope == SkillScopeUser || scope == SkillScopeAdmin

	visited := map[abspath.AbsolutePathBuf]struct{}{root: {}}
	queue := []dirQueueItem{{path: root, depth: 0}}

	enqueue := func(path abspath.AbsolutePathBuf, depth int) {
		if depth > maxScanDepth {
			return
		}
		if len(visited) >= maxSkillsDirsPerRoot {
			return
		}
		if _, ok := visited[path]; ok {
			return
		}
		visited[path] = struct{}{}
		queue = append(queue, dirQueueItem{path: path, depth: depth})
	}

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		dir, depth := item.path, item.depth

		entries, err := fs.ReadDirectory(ctx, dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			fileName := entry.FileName
			if strings.HasPrefix(fileName, ".") {
				continue
			}
			path := dir.Join(fileName)
			entryMeta, err := fs.GetMetadata(ctx, path)
			if err != nil {
				continue
			}

			if entryMeta.IsSymlink {
				if !followSymlinks {
					continue
				}
				if _, derr := fs.ReadDirectory(ctx, path); derr == nil {
					enqueue(canonicalizeForSkillIdentity(path), depth+1)
				}
				continue
			}

			if entryMeta.IsDirectory {
				enqueue(canonicalizeForSkillIdentity(path), depth+1)
				continue
			}

			if entryMeta.IsFile && fileName == skillsFilename {
				skill, perr := parseSkillFile(ctx, fs, path, scope, pluginID, resolvedPluginRoot)
				if perr != nil {
					if scope != SkillScopeSystem {
						outcome.Errors = append(outcome.Errors, SkillError{
							Path:    path,
							Message: perr.Error(),
						})
					}
					continue
				}
				outcome.Skills = append(outcome.Skills, skill)
			}
		}
	}
}

// parseSkillFile reads and validates a SKILL.md, returning its metadata. It
// mirrors the Rust `parse_skill_file`.
func parseSkillFile(
	ctx context.Context,
	fs FileSystem,
	path abspath.AbsolutePathBuf,
	scope SkillScope,
	pluginID *string,
	pluginRoot *abspath.AbsolutePathBuf,
) (SkillMetadata, error) {
	contents, err := fs.ReadFileText(ctx, path)
	if err != nil {
		return SkillMetadata{}, &skillParseError{kind: parseErrRead, cause: err}
	}

	frontmatter, ok := extractFrontmatter(contents)
	if !ok {
		return SkillMetadata{}, &skillParseError{kind: parseErrMissingFrontmatter}
	}

	parsed, err := parseYAML(frontmatter)
	if err != nil {
		return SkillMetadata{}, &skillParseError{kind: parseErrInvalidYaml, cause: errors.Unwrap(err)}
	}

	baseName := defaultSkillName(path)
	if nameValue, ok := parsed.get("name"); ok {
		if raw, ok := nameValue.asString(); ok {
			if sanitized := sanitizeSingleLine(raw); sanitized != "" {
				baseName = sanitized
			}
		}
	}
	name := namespacedSkillName(ctx, fs, path, baseName)

	description := ""
	if descValue, ok := parsed.get("description"); ok {
		if raw, ok := descValue.asString(); ok {
			description = sanitizeSingleLine(raw)
		}
	}

	var shortDescription *string
	if metaValue, ok := parsed.get("metadata"); ok {
		if sdValue, ok := metaValue.get("short-description"); ok {
			if raw, ok := sdValue.asString(); ok {
				if sanitized := sanitizeSingleLine(raw); sanitized != "" {
					shortDescription = &sanitized
				}
			}
		}
	}

	loaded := loadSkillMetadata(ctx, fs, path, pluginRoot)

	if err := validateLen(name, maxNameLen, "name"); err != nil {
		return SkillMetadata{}, err
	}
	if err := validateLen(description, maxDescriptionLen, "description"); err != nil {
		return SkillMetadata{}, err
	}
	if shortDescription != nil {
		if err := validateLen(*shortDescription, maxShortDescriptionLen, "metadata.short-description"); err != nil {
			return SkillMetadata{}, err
		}
	}

	return SkillMetadata{
		Name:             name,
		Description:      description,
		ShortDescription: shortDescription,
		Interface:        loaded.iface,
		Dependencies:     loaded.dependencies,
		Policy:           loaded.policy,
		PathToSkillsMd:   canonicalizeForSkillIdentity(path),
		Scope:            scope,
		PluginID:         cloneStringPtr(pluginID),
	}, nil
}

// defaultSkillName derives a skill name from the SKILL.md's parent directory,
// mirroring the Rust `default_skill_name`.
func defaultSkillName(path abspath.AbsolutePathBuf) string {
	if parent, ok := path.Parent(); ok {
		base := filepath.Base(parent.String())
		if sanitized := sanitizeSingleLine(base); sanitized != "" && base != string(filepath.Separator) {
			return sanitized
		}
	}
	return "skill"
}

// namespacedSkillName prefixes baseName with the plugin namespace when the skill
// lives under a plugin manifest, mirroring the Rust `namespaced_skill_name`.
func namespacedSkillName(ctx context.Context, fs FileSystem, path abspath.AbsolutePathBuf, baseName string) string {
	if namespace, ok := pluginutil.PluginNamespaceForSkillPath(ctx, asPluginFS(fs), path); ok {
		return namespace + ":" + baseName
	}
	return baseName
}

// loadedSkillMetadata bundles the optional sidecar-derived fields.
type loadedSkillMetadata struct {
	iface        *SkillInterface
	dependencies *SkillDependencies
	policy       *SkillPolicy
}

// loadSkillMetadata reads the optional agents/openai.yaml sidecar next to a
// SKILL.md. It fails open: a missing, unreadable, or invalid sidecar yields no
// metadata rather than an error, mirroring the Rust `load_skill_metadata`.
func loadSkillMetadata(ctx context.Context, fs FileSystem, skillPath abspath.AbsolutePathBuf, pluginRoot *abspath.AbsolutePathBuf) loadedSkillMetadata {
	skillDir, ok := skillPath.Parent()
	if !ok {
		return loadedSkillMetadata{}
	}
	metadataPath := skillDir.Join(skillsMetadataDir).Join(skillsMetadataFilename)
	metadata, err := fs.GetMetadata(ctx, metadataPath)
	if err != nil || !metadata.IsFile {
		return loadedSkillMetadata{}
	}
	contents, err := fs.ReadFileText(ctx, metadataPath)
	if err != nil {
		return loadedSkillMetadata{}
	}
	parsed, err := parseYAML(contents)
	if err != nil {
		return loadedSkillMetadata{}
	}

	var out loadedSkillMetadata
	if ifaceValue, ok := parsed.get("interface"); ok && ifaceValue.kind == yamlMapping {
		out.iface = resolveInterface(ifaceValue, skillDir, pluginRoot)
	}
	if depsValue, ok := parsed.get("dependencies"); ok && depsValue.kind == yamlMapping {
		out.dependencies = resolveDependencies(depsValue)
	}
	if policyValue, ok := parsed.get("policy"); ok && policyValue.kind == yamlMapping {
		out.policy = resolvePolicy(policyValue)
	}
	return out
}

// extractFrontmatter returns the YAML front-matter delimited by leading and
// trailing "---" lines, mirroring the Rust `extract_frontmatter`. It returns
// ok=false when the front-matter is missing or unterminated.
func extractFrontmatter(contents string) (string, bool) {
	lines := strings.Split(contents, "\n")
	if len(lines) == 0 {
		return "", false
	}
	if strings.TrimSpace(stripCR(lines[0])) != "---" {
		return "", false
	}
	var frontmatter []string
	foundClosing := false
	for _, line := range lines[1:] {
		if strings.TrimSpace(stripCR(line)) == "---" {
			foundClosing = true
			break
		}
		frontmatter = append(frontmatter, stripCR(line))
	}
	if len(frontmatter) == 0 || !foundClosing {
		return "", false
	}
	return strings.Join(frontmatter, "\n"), true
}

func stripCR(s string) string { return strings.TrimRight(s, "\r") }

// sanitizeSingleLine collapses all runs of whitespace into single spaces,
// mirroring the Rust `sanitize_single_line`.
func sanitizeSingleLine(raw string) string {
	return strings.Join(strings.Fields(raw), " ")
}

// validateLen enforces non-empty and maximum-character-length constraints on a
// required string field, mirroring the Rust `validate_len`. Lengths count
// Unicode scalar values, not bytes.
func validateLen(value string, maxLen int, field string) error {
	if value == "" {
		return &skillParseError{kind: parseErrMissingField, field: field}
	}
	if utf8.RuneCountInString(value) > maxLen {
		return &skillParseError{
			kind:   parseErrInvalidField,
			field:  field,
			reason: fmt.Sprintf("exceeds maximum length of %d characters", maxLen),
		}
	}
	return nil
}

func cloneStringPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

// isNotExist reports whether err indicates a missing path.
func isNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
