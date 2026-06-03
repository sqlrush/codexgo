package sandbox

import (
	"os"
	"path"
	"sort"
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// Protected workspace metadata path names that stay read-only under writable
// roots. Mirrors PROTECTED_METADATA_* in codex-rs/protocol/src/permissions.rs.
const (
	protectedMetadataGitName    = ".git"
	protectedMetadataAgentsName = ".agents"
	protectedMetadataCodexName  = ".codex"
)

// protectedMetadataPathNames lists the protected metadata basenames in the same
// order as the Rust PROTECTED_METADATA_PATH_NAMES slice.
var protectedMetadataPathNames = []string{
	protectedMetadataGitName,
	protectedMetadataAgentsName,
	protectedMetadataCodexName,
}

// resolvedEntry is a filesystem sandbox entry whose path token has been resolved
// to a concrete absolute path against a working directory. Glob and unresolvable
// special entries are dropped, mirroring resolved_entries_with_cwd.
type resolvedEntry struct {
	path   string
	access protocol.FileSystemAccessMode
}

// fsHasFullDiskReadAccess reports whether filesystem reads are unrestricted.
// Mirrors FileSystemSandboxPolicy::has_full_disk_read_access.
func fsHasFullDiskReadAccess(p protocol.FileSystemSandboxPolicy) bool {
	switch p.Kind {
	case protocol.FileSystemSandboxKindUnrestricted, protocol.FileSystemSandboxKindExternalSandbox:
		return true
	default: // Restricted
		return fsHasRootAccess(p, protocol.FileSystemAccessMode.CanRead) && !fsHasDeniedReadRestrictions(p)
	}
}

// fsHasFullDiskWriteAccess reports whether filesystem writes are unrestricted.
// Mirrors FileSystemSandboxPolicy::has_full_disk_write_access.
func fsHasFullDiskWriteAccess(p protocol.FileSystemSandboxPolicy) bool {
	switch p.Kind {
	case protocol.FileSystemSandboxKindUnrestricted, protocol.FileSystemSandboxKindExternalSandbox:
		return true
	default: // Restricted
		return fsHasRootAccess(p, protocol.FileSystemAccessMode.CanWrite) && !fsHasWriteNarrowingEntries(p)
	}
}

// fsIncludePlatformDefaults reports whether the macOS platform-default readable
// roots should be appended. Mirrors include_platform_defaults.
func fsIncludePlatformDefaults(p protocol.FileSystemSandboxPolicy) bool {
	if fsHasFullDiskReadAccess(p) || p.Kind != protocol.FileSystemSandboxKindRestricted {
		return false
	}
	for _, entry := range p.Entries {
		if entry.Path.Type == protocol.FileSystemPathTypeSpecial &&
			entry.Path.Special != nil &&
			entry.Path.Special.Kind == protocol.FileSystemSpecialPathKindMinimal &&
			entry.Access.CanRead() {
			return true
		}
	}
	return false
}

// fsHasRootAccess reports whether a Restricted policy carries a Special(Root)
// entry whose access satisfies predicate. Mirrors has_root_access.
func fsHasRootAccess(p protocol.FileSystemSandboxPolicy, predicate func(protocol.FileSystemAccessMode) bool) bool {
	if p.Kind != protocol.FileSystemSandboxKindRestricted {
		return false
	}
	for _, entry := range p.Entries {
		if entry.Path.Type == protocol.FileSystemPathTypeSpecial &&
			entry.Path.Special != nil &&
			entry.Path.Special.Kind == protocol.FileSystemSpecialPathKindRoot &&
			predicate(entry.Access) {
			return true
		}
	}
	return false
}

// fsHasDeniedReadRestrictions reports whether a Restricted policy carries any
// Deny entry. Mirrors has_denied_read_restrictions.
func fsHasDeniedReadRestrictions(p protocol.FileSystemSandboxPolicy) bool {
	if p.Kind != protocol.FileSystemSandboxKindRestricted {
		return false
	}
	for _, entry := range p.Entries {
		if entry.Access == protocol.FileSystemAccessModeDeny {
			return true
		}
	}
	return false
}

// fsHasWriteNarrowingEntries mirrors has_write_narrowing_entries.
func fsHasWriteNarrowingEntries(p protocol.FileSystemSandboxPolicy) bool {
	if p.Kind != protocol.FileSystemSandboxKindRestricted {
		return false
	}
	for i := range p.Entries {
		entry := p.Entries[i]
		if entry.Access.CanWrite() {
			continue
		}
		switch entry.Path.Type {
		case protocol.FileSystemPathTypePath:
			if !fsHasSameTargetWriteOverride(p, entry) {
				return true
			}
		case protocol.FileSystemPathTypeGlobPattern:
			return true
		case protocol.FileSystemPathTypeSpecial:
			if entry.Path.Special == nil {
				continue
			}
			switch entry.Path.Special.Kind {
			case protocol.FileSystemSpecialPathKindRoot:
				if entry.Access == protocol.FileSystemAccessModeDeny {
					return true
				}
			case protocol.FileSystemSpecialPathKindMinimal, protocol.FileSystemSpecialPathKindUnknown:
				// never narrows
			default:
				if !fsHasSameTargetWriteOverride(p, entry) {
					return true
				}
			}
		}
	}
	return false
}

// fsHasSameTargetWriteOverride mirrors has_same_target_write_override.
func fsHasSameTargetWriteOverride(p protocol.FileSystemSandboxPolicy, entry protocol.FileSystemSandboxEntry) bool {
	for i := range p.Entries {
		candidate := p.Entries[i]
		if candidate.Access.CanWrite() &&
			accessGreater(candidate.Access, entry.Access) &&
			pathsShareTarget(candidate.Path, entry.Path) {
			return true
		}
	}
	return false
}

// accessGreater mirrors the Rust Ord on FileSystemAccessMode: Read < Write < Deny.
func accessGreater(a, b protocol.FileSystemAccessMode) bool {
	return accessRank(a) > accessRank(b)
}

func accessRank(a protocol.FileSystemAccessMode) int {
	switch a {
	case protocol.FileSystemAccessModeRead:
		return 0
	case protocol.FileSystemAccessModeWrite:
		return 1
	default: // Deny
		return 2
	}
}

// resolveAccessWithCwd mirrors resolve_access_with_cwd.
func resolveAccessWithCwd(p protocol.FileSystemSandboxPolicy, target, cwd string) protocol.FileSystemAccessMode {
	switch p.Kind {
	case protocol.FileSystemSandboxKindUnrestricted, protocol.FileSystemSandboxKindExternalSandbox:
		return protocol.FileSystemAccessModeWrite
	}

	resolved, ok := resolveCandidatePath(target, cwd)
	if !ok {
		return protocol.FileSystemAccessModeDeny
	}

	var best *resolvedEntry
	bestSpec := -1
	for _, entry := range resolvedEntriesWithCwd(p, cwd) {
		entry := entry
		if !pathStartsWith(resolved, entry.path) {
			continue
		}
		spec := componentCount(entry.path)
		if best == nil || spec > bestSpec || (spec == bestSpec && accessGreater(entry.access, best.access)) {
			b := entry
			best = &b
			bestSpec = spec
		}
	}
	if best == nil {
		return protocol.FileSystemAccessModeDeny
	}
	return best.access
}

// canReadPathWithCwd mirrors can_read_path_with_cwd.
func canReadPathWithCwd(p protocol.FileSystemSandboxPolicy, target, cwd string) bool {
	return resolveAccessWithCwd(p, target, cwd).CanRead()
}

// canWritePathWithCwd mirrors can_write_path_with_cwd, including the protected
// metadata write carveout.
func canWritePathWithCwd(p protocol.FileSystemSandboxPolicy, target, cwd string) bool {
	if !resolveAccessWithCwd(p, target, cwd).CanWrite() {
		return false
	}
	if fsHasFullDiskWriteAccess(p) {
		return true
	}
	return !isMetadataWriteDenied(p, target, cwd)
}

// isMetadataWriteDenied mirrors is_metadata_write_denied.
func isMetadataWriteDenied(p protocol.FileSystemSandboxPolicy, target, cwd string) bool {
	if p.Kind != protocol.FileSystemSandboxKindRestricted {
		return false
	}
	resolvedTarget, ok := resolveCandidatePath(target, cwd)
	if !ok {
		return true
	}
	protectedPath, _, ok := metadataChildOfWritableRoot(p, resolvedTarget, cwd)
	if !ok {
		return false
	}
	// Mirrors Rust is_metadata_write_denied, which returns the negation of
	// has_explicit_write_entry_for_metadata_path. The earlier Go port recursed
	// back into canWritePathWithCwd(protectedPath) here, but a protected metadata
	// path is itself a metadata child of the same writable root, so that produced
	// unbounded recursion (stack overflow) for the default protected-metadata
	// case. The Rust reference does not recurse.
	return !hasExplicitWriteEntryForMetadataPath(p, protectedPath, resolvedTarget, cwd)
}

// metadataChildOfWritableRoot mirrors metadata_child_of_writable_root.
func metadataChildOfWritableRoot(p protocol.FileSystemSandboxPolicy, target, cwd string) (string, string, bool) {
	for _, entry := range resolvedEntriesWithCwd(p, cwd) {
		if !entry.access.CanWrite() {
			continue
		}
		rel, ok := stripPrefixPath(target, entry.path)
		if !ok {
			continue
		}
		first := firstComponent(rel)
		if first == "" {
			continue
		}
		name := metadataPathName(first)
		if name == "" {
			continue
		}
		return path.Join(entry.path, name), name, true
	}
	return "", "", false
}

// hasExplicitWriteEntryForMetadataPath mirrors has_explicit_write_entry_for_metadata_path.
func hasExplicitWriteEntryForMetadataPath(p protocol.FileSystemSandboxPolicy, protectedPath, target, cwd string) bool {
	for _, entry := range resolvedEntriesWithCwd(p, cwd) {
		if entry.access.CanWrite() &&
			pathStartsWith(target, entry.path) &&
			pathStartsWith(entry.path, protectedPath) {
			return true
		}
	}
	return false
}

// metadataPathName returns the canonical protected metadata name matching the
// component, or "" when none match.
func metadataPathName(component string) string {
	for _, name := range protectedMetadataPathNames {
		if component == name {
			return name
		}
	}
	return ""
}

// getReadableRootsWithCwd mirrors get_readable_roots_with_cwd.
func getReadableRootsWithCwd(p protocol.FileSystemSandboxPolicy, cwd string) []string {
	if fsHasFullDiskReadAccess(p) {
		return nil
	}
	var paths []string
	for _, entry := range resolvedEntriesWithCwd(p, cwd) {
		if !entry.access.CanRead() {
			continue
		}
		if !canReadPathWithCwd(p, entry.path, cwd) {
			continue
		}
		paths = append(paths, entry.path)
	}
	return dedupPaths(paths)
}

// getWritableRootsWithCwd mirrors get_writable_roots_with_cwd. This is a
// simplified port: it computes writable roots, default read-only metadata
// carveouts (.git/.agents/.codex), explicit deny carveouts that fall under a
// root, and protected metadata names. The symlink-preservation edge cases from
// the Rust implementation are not reproduced (see package deviations note).
func getWritableRootsWithCwd(p protocol.FileSystemSandboxPolicy, cwd string) []protocol.WritableRoot {
	if fsHasFullDiskWriteAccess(p) {
		return nil
	}

	resolvedEntries := resolvedEntriesWithCwd(p, cwd)
	var writableEntries []string
	for _, entry := range resolvedEntries {
		if !entry.access.CanWrite() {
			continue
		}
		if !canWritePathWithCwd(p, entry.path, cwd) {
			continue
		}
		writableEntries = append(writableEntries, entry.path)
	}

	roots := dedupPaths(writableEntries)
	out := make([]protocol.WritableRoot, 0, len(roots))
	cwdAbs, cwdOK := resolveCandidatePath(cwd, cwd)
	for _, root := range roots {
		protectedNames := protectedMetadataNamesForWritableRoot(p, root, cwd)
		protectMissingDotCodex := cwdOK && cwdAbs == root
		var readOnly []string
		for _, sub := range defaultReadOnlySubpathsForWritableRoot(root, protectMissingDotCodex) {
			if !hasExplicitResolvedPathEntry(resolvedEntries, sub) {
				readOnly = append(readOnly, sub)
			}
		}
		for _, entry := range resolvedEntries {
			if entry.access.CanWrite() {
				continue
			}
			if canWritePathWithCwd(p, entry.path, cwd) {
				continue
			}
			if entry.path == root || !pathStartsWith(entry.path, root) {
				continue
			}
			readOnly = append(readOnly, entry.path)
		}
		out = append(out, protocol.WritableRoot{
			Root:                   protocol.AbsolutePath(root),
			ReadOnlySubpaths:       toAbsolutePaths(dedupPathsNoNormalize(readOnly)),
			ProtectedMetadataNames: protectedNames,
		})
	}
	return out
}

// getUnreadableRootsWithCwd mirrors get_unreadable_roots_with_cwd.
func getUnreadableRootsWithCwd(p protocol.FileSystemSandboxPolicy, cwd string) []string {
	if p.Kind != protocol.FileSystemSandboxKindRestricted {
		return nil
	}
	rootPath, rootOK := "", false
	if cwdAbs, ok := resolveCandidatePath(cwd, cwd); ok {
		rootPath = filesystemRootForPath(cwdAbs)
		rootOK = true
	}
	var paths []string
	for _, entry := range resolvedEntriesWithCwd(p, cwd) {
		if entry.access != protocol.FileSystemAccessModeDeny {
			continue
		}
		if canReadPathWithCwd(p, entry.path, cwd) {
			continue
		}
		if rootOK && entry.path == rootPath {
			continue
		}
		paths = append(paths, entry.path)
	}
	return dedupPaths(paths)
}

// getUnreadableGlobsWithCwd mirrors get_unreadable_globs_with_cwd.
func getUnreadableGlobsWithCwd(p protocol.FileSystemSandboxPolicy, cwd string) []string {
	if p.Kind != protocol.FileSystemSandboxKindRestricted {
		return nil
	}
	var patterns []string
	for _, entry := range p.Entries {
		if entry.Access != protocol.FileSystemAccessModeDeny {
			continue
		}
		if entry.Path.Type != protocol.FileSystemPathTypeGlobPattern {
			continue
		}
		patterns = append(patterns, resolvePathAgainstBase(entry.Path.Pattern, cwd))
	}
	sort.Strings(patterns)
	return dedupSortedStrings(patterns)
}

// resolvedEntriesWithCwd mirrors resolved_entries_with_cwd.
func resolvedEntriesWithCwd(p protocol.FileSystemSandboxPolicy, cwd string) []resolvedEntry {
	cwdAbs, cwdOK := resolveCandidatePath(cwd, cwd)
	out := make([]resolvedEntry, 0, len(p.Entries))
	for _, entry := range p.Entries {
		resolved, ok := resolveEntryPath(entry.Path, cwdAbs, cwdOK)
		if !ok {
			continue
		}
		out = append(out, resolvedEntry{path: resolved, access: entry.Access})
	}
	return out
}

// resolveEntryPath mirrors resolve_entry_path: Special(Root) resolves to the
// filesystem root of cwd; everything else uses resolve_file_system_path.
func resolveEntryPath(p protocol.FileSystemPath, cwdAbs string, cwdOK bool) (string, bool) {
	if p.Type == protocol.FileSystemPathTypeSpecial && p.Special != nil &&
		p.Special.Kind == protocol.FileSystemSpecialPathKindRoot {
		if !cwdOK {
			return "", false
		}
		return filesystemRootForPath(cwdAbs), true
	}
	return resolveFileSystemPath(p, cwdAbs, cwdOK)
}

// resolveFileSystemPath mirrors resolve_file_system_path.
func resolveFileSystemPath(p protocol.FileSystemPath, cwdAbs string, cwdOK bool) (string, bool) {
	switch p.Type {
	case protocol.FileSystemPathTypePath:
		return string(p.Path), true
	case protocol.FileSystemPathTypeGlobPattern:
		return "", false
	case protocol.FileSystemPathTypeSpecial:
		if p.Special == nil {
			return "", false
		}
		return resolveSpecialPath(*p.Special, cwdAbs, cwdOK)
	}
	return "", false
}

// resolveSpecialPath mirrors resolve_file_system_special_path.
func resolveSpecialPath(s protocol.FileSystemSpecialPath, cwdAbs string, cwdOK bool) (string, bool) {
	switch s.Kind {
	case protocol.FileSystemSpecialPathKindRoot,
		protocol.FileSystemSpecialPathKindMinimal,
		protocol.FileSystemSpecialPathKindUnknown:
		return "", false
	case protocol.FileSystemSpecialPathKindProjectRoots:
		if !cwdOK {
			return "", false
		}
		if s.Subpath != nil {
			return resolvePathAgainstBase(*s.Subpath, cwdAbs), true
		}
		return cwdAbs, true
	case protocol.FileSystemSpecialPathKindTmpdir:
		tmpdir := os.Getenv("TMPDIR")
		if tmpdir == "" {
			return "", false
		}
		return normalizeAbsolute(tmpdir), true
	case protocol.FileSystemSpecialPathKindSlashTmp:
		if info, err := os.Stat("/tmp"); err != nil || !info.IsDir() {
			return "", false
		}
		return "/tmp", true
	}
	return "", false
}

// protectedMetadataNamesForWritableRoot mirrors protected_metadata_names_for_writable_root
// (single-root form; the raw_writable_roots argument is folded into root here).
func protectedMetadataNamesForWritableRoot(p protocol.FileSystemSandboxPolicy, root, cwd string) []string {
	var names []string
	for _, name := range protectedMetadataPathNames {
		metadataPath := path.Join(root, name)
		if !canWritePathWithCwd(p, metadataPath, cwd) {
			names = append(names, name)
		}
	}
	return names
}

// defaultReadOnlySubpathsForWritableRoot mirrors
// default_read_only_subpaths_for_writable_root. Git pointer-file gitdir
// resolution is omitted (see package deviations note).
func defaultReadOnlySubpathsForWritableRoot(root string, protectMissingDotCodex bool) []string {
	var subpaths []string
	topGit := path.Join(root, protectedMetadataGitName)
	if isDir(topGit) || isFile(topGit) {
		subpaths = append(subpaths, topGit)
	}
	topAgents := path.Join(root, protectedMetadataAgentsName)
	if isDir(topAgents) {
		subpaths = append(subpaths, topAgents)
	}
	topCodex := path.Join(root, protectedMetadataCodexName)
	if protectMissingDotCodex || isDir(topCodex) {
		subpaths = append(subpaths, topCodex)
	}
	return dedupPathsNoNormalize(subpaths)
}

// hasExplicitResolvedPathEntry mirrors has_explicit_resolved_path_entry.
func hasExplicitResolvedPathEntry(entries []resolvedEntry, target string) bool {
	for _, entry := range entries {
		if entry.path == target {
			return true
		}
	}
	return false
}

// pathsShareTarget mirrors file_system_paths_share_target.
func pathsShareTarget(left, right protocol.FileSystemPath) bool {
	switch {
	case left.Type == protocol.FileSystemPathTypePath && right.Type == protocol.FileSystemPathTypePath:
		return left.Path == right.Path
	case left.Type == protocol.FileSystemPathTypeSpecial && right.Type == protocol.FileSystemPathTypeSpecial:
		return specialPathsShareTarget(left.Special, right.Special)
	case left.Type == protocol.FileSystemPathTypePath && right.Type == protocol.FileSystemPathTypeSpecial:
		return specialMatchesAbsolutePath(right.Special, string(left.Path))
	case left.Type == protocol.FileSystemPathTypeSpecial && right.Type == protocol.FileSystemPathTypePath:
		return specialMatchesAbsolutePath(left.Special, string(right.Path))
	case left.Type == protocol.FileSystemPathTypeGlobPattern && right.Type == protocol.FileSystemPathTypeGlobPattern:
		return left.Pattern == right.Pattern
	default:
		return false
	}
}

// specialPathsShareTarget mirrors special_paths_share_target.
func specialPathsShareTarget(left, right *protocol.FileSystemSpecialPath) bool {
	if left == nil || right == nil {
		return false
	}
	if left.Kind != right.Kind {
		return false
	}
	switch left.Kind {
	case protocol.FileSystemSpecialPathKindRoot,
		protocol.FileSystemSpecialPathKindMinimal,
		protocol.FileSystemSpecialPathKindTmpdir,
		protocol.FileSystemSpecialPathKindSlashTmp:
		return true
	case protocol.FileSystemSpecialPathKindProjectRoots:
		return ptrStringEqual(left.Subpath, right.Subpath)
	case protocol.FileSystemSpecialPathKindUnknown:
		return left.Path == right.Path && ptrStringEqual(left.Subpath, right.Subpath)
	}
	return false
}

// specialMatchesAbsolutePath mirrors special_path_matches_absolute_path.
func specialMatchesAbsolutePath(value *protocol.FileSystemSpecialPath, target string) bool {
	if value == nil {
		return false
	}
	switch value.Kind {
	case protocol.FileSystemSpecialPathKindRoot:
		return parentless(target)
	case protocol.FileSystemSpecialPathKindSlashTmp:
		return target == "/tmp"
	}
	return false
}

func ptrStringEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// toLegacySandboxPolicy mirrors FileSystemSandboxPolicy::to_legacy_sandbox_policy.
func toLegacySandboxPolicy(p protocol.FileSystemSandboxPolicy, network protocol.NetworkSandboxPolicy, cwd string) (protocol.SandboxPolicy, error) {
	switch p.Kind {
	case protocol.FileSystemSandboxKindExternalSandbox:
		return externalSandboxPolicy(network), nil
	case protocol.FileSystemSandboxKindUnrestricted:
		if network.IsEnabled() {
			return protocol.NewDangerFullAccessSandboxPolicy(), nil
		}
		return externalSandboxPolicy(protocol.NetworkSandboxPolicyRestricted), nil
	}

	cwdAbs, cwdOK := resolveCandidatePath(cwd, cwd)
	hasFullWrite := fsHasFullDiskWriteAccess(p)
	workspaceRootWritable := false
	var writableRoots []string
	tmpdirWritable := false
	slashTmpWritable := false
	unbridgeableRootWrite := false

	for _, entry := range p.Entries {
		switch entry.Path.Type {
		case protocol.FileSystemPathTypeGlobPattern:
			// ignored
		case protocol.FileSystemPathTypePath:
			if entry.Access.CanWrite() {
				if cwdOK && cwdAbs == string(entry.Path.Path) {
					workspaceRootWritable = true
				} else {
					writableRoots = append(writableRoots, string(entry.Path.Path))
				}
			}
		case protocol.FileSystemPathTypeSpecial:
			if entry.Path.Special == nil {
				continue
			}
			switch entry.Path.Special.Kind {
			case protocol.FileSystemSpecialPathKindRoot:
				if entry.Access == protocol.FileSystemAccessModeWrite {
					unbridgeableRootWrite = true
				}
			case protocol.FileSystemSpecialPathKindProjectRoots:
				if entry.Path.Special.Subpath == nil && entry.Access.CanWrite() {
					workspaceRootWritable = true
				} else if resolved, ok := resolveSpecialPath(*entry.Path.Special, cwdAbs, cwdOK); ok && entry.Access.CanWrite() {
					writableRoots = append(writableRoots, resolved)
				}
			case protocol.FileSystemSpecialPathKindTmpdir:
				if entry.Access.CanWrite() {
					tmpdirWritable = true
				}
			case protocol.FileSystemSpecialPathKindSlashTmp:
				if entry.Access.CanWrite() {
					slashTmpWritable = true
				}
			}
		}
	}

	if hasFullWrite {
		if network.IsEnabled() {
			return protocol.NewDangerFullAccessSandboxPolicy(), nil
		}
		return externalSandboxPolicy(protocol.NetworkSandboxPolicyRestricted), nil
	}

	switch {
	case workspaceRootWritable:
		return protocol.SandboxPolicy{
			Type:                protocol.SandboxPolicyTypeWorkspaceWrite,
			WritableRoots:       toAbsolutePaths(dedupPathsNoNormalize(writableRoots)),
			NetworkAccessBool:   network.IsEnabled(),
			ExcludeTmpdirEnvVar: !tmpdirWritable,
			ExcludeSlashTmp:     !slashTmpWritable,
		}, nil
	case unbridgeableRootWrite || len(writableRoots) > 0 || tmpdirWritable || slashTmpWritable:
		return protocol.SandboxPolicy{}, errLegacyWriteOutsideWorkspace
	default:
		return protocol.NewReadOnlySandboxPolicy(network.IsEnabled()), nil
	}
}

func externalSandboxPolicy(network protocol.NetworkSandboxPolicy) protocol.SandboxPolicy {
	na := protocol.NetworkAccessRestricted
	if network.IsEnabled() {
		na = protocol.NetworkAccessEnabled
	}
	return protocol.SandboxPolicy{Type: protocol.SandboxPolicyTypeExternalSandbox, NetworkAccess: na}
}

func toAbsolutePaths(paths []string) []protocol.AbsolutePath {
	if len(paths) == 0 {
		return nil
	}
	out := make([]protocol.AbsolutePath, 0, len(paths))
	for _, p := range paths {
		out = append(out, protocol.AbsolutePath(p))
	}
	return out
}

func dedupSortedStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := in[:0:0]
	var last string
	for i, s := range in {
		if i == 0 || s != last {
			out = append(out, s)
			last = s
		}
	}
	return out
}

// firstComponent returns the first path component of a relative path, or "".
func firstComponent(rel string) string {
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return ""
	}
	if idx := strings.IndexByte(rel, '/'); idx >= 0 {
		return rel[:idx]
	}
	return rel
}
