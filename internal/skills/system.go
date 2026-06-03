package skills

import (
	"embed"
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// systemSkillsFS embeds the built-in (system) skills shipped with Codex. The
// directory layout under assets/samples mirrors the Rust
// `include_dir!("$CARGO_MANIFEST_DIR/src/assets/samples")` so the installed
// on-disk files match byte-for-byte.
//
//go:embed all:assets/samples
var systemSkillsFS embed.FS

const (
	systemSkillsEmbedRoot     = "assets/samples"
	systemSkillsDirName       = ".system"
	skillsDirName             = "skills"
	systemSkillsMarkerName    = ".codex-system-skills.marker"
	systemSkillsMarkerSalt    = "v1"
	systemSkillsCacheDirPerms = 0o755
	systemSkillsFilePerms     = 0o644
)

// SystemCacheRootDir returns the on-disk cache location for embedded system
// skills under an absolute CODEX_HOME. It mirrors the Rust
// `system_cache_root_dir`.
func SystemCacheRootDir(codexHome abspath.AbsolutePathBuf) abspath.AbsolutePathBuf {
	return codexHome.Join(skillsDirName).Join(systemSkillsDirName)
}

// InstallSystemSkills installs the embedded system skills into
// CODEX_HOME/skills/.system, clearing any existing system skills directory
// first. A marker file with a fingerprint of the embedded directory lets repeated
// startups skip the work when nothing changed. It mirrors the Rust
// `install_system_skills`.
func InstallSystemSkills(codexHome abspath.AbsolutePathBuf) error {
	skillsRoot := codexHome.Join(skillsDirName)
	if err := os.MkdirAll(skillsRoot.Path(), systemSkillsCacheDirPerms); err != nil {
		return fmt.Errorf("skills: create skills root dir: %w", err)
	}

	destSystem := SystemCacheRootDir(codexHome)
	markerPath := destSystem.Join(systemSkillsMarkerName)
	expected := embeddedSystemSkillsFingerprint()

	if info, err := os.Stat(destSystem.Path()); err == nil && info.IsDir() {
		if existing, err := readMarker(markerPath); err == nil && existing == expected {
			return nil
		}
	}

	if _, err := os.Stat(destSystem.Path()); err == nil {
		if err := os.RemoveAll(destSystem.Path()); err != nil {
			return fmt.Errorf("skills: remove existing system skills dir: %w", err)
		}
	}

	if err := writeEmbeddedDir(destSystem); err != nil {
		return err
	}
	if err := os.WriteFile(markerPath.Path(), []byte(expected+"\n"), systemSkillsFilePerms); err != nil {
		return fmt.Errorf("skills: write system skills marker: %w", err)
	}
	return nil
}

// UninstallSystemSkills removes the cached system skills directory. It mirrors
// the Rust `uninstall_system_skills` (best-effort; errors are ignored).
func UninstallSystemSkills(codexHome abspath.AbsolutePathBuf) {
	_ = os.RemoveAll(SystemCacheRootDir(codexHome).Path())
}

func readMarker(path abspath.AbsolutePathBuf) (string, error) {
	data, err := os.ReadFile(path.Path())
	if err != nil {
		return "", fmt.Errorf("skills: read system skills marker: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// embeddedSystemSkillsFingerprint produces a stable fingerprint of the embedded
// system skills tree. The Rust implementation uses SipHash via DefaultHasher;
// that exact value cannot be reproduced cross-language, but the marker is an
// internal reinstall optimization (not a wire/on-disk format consumed elsewhere),
// so we use a deterministic FNV-1a fingerprint over sorted (path, content) pairs
// with the same salt and ordering rules.
func embeddedSystemSkillsFingerprint() string {
	type item struct {
		path        string
		contentHash uint64
		isDir       bool
	}
	var items []item
	_ = fs.WalkDir(systemSkillsFS, systemSkillsEmbedRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == systemSkillsEmbedRoot {
			return nil
		}
		rel := strings.TrimPrefix(path, systemSkillsEmbedRoot+"/")
		if entry.IsDir() {
			items = append(items, item{path: rel, isDir: true})
			return nil
		}
		data, err := systemSkillsFS.ReadFile(path)
		if err != nil {
			return err
		}
		h := fnv.New64a()
		_, _ = h.Write(data)
		items = append(items, item{path: rel, contentHash: h.Sum64()})
		return nil
	})
	sort.Slice(items, func(i, j int) bool { return items[i].path < items[j].path })

	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(systemSkillsMarkerSalt))
	for _, it := range items {
		_, _ = hasher.Write([]byte(it.path))
		if !it.isDir {
			var buf [8]byte
			h := it.contentHash
			for i := 0; i < 8; i++ {
				buf[i] = byte(h >> (8 * i))
			}
			_, _ = hasher.Write(buf[:])
		}
	}
	return fmt.Sprintf("%x", hasher.Sum64())
}

// writeEmbeddedDir writes the embedded system skills tree to dest, preserving
// structure. It mirrors the Rust `write_embedded_dir`.
func writeEmbeddedDir(dest abspath.AbsolutePathBuf) error {
	if err := os.MkdirAll(dest.Path(), systemSkillsCacheDirPerms); err != nil {
		return fmt.Errorf("skills: create system skills dir: %w", err)
	}
	return fs.WalkDir(systemSkillsFS, systemSkillsEmbedRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == systemSkillsEmbedRoot {
			return nil
		}
		rel := strings.TrimPrefix(path, systemSkillsEmbedRoot+"/")
		target := dest.Join(filepath.FromSlash(rel))
		if entry.IsDir() {
			if err := os.MkdirAll(target.Path(), systemSkillsCacheDirPerms); err != nil {
				return fmt.Errorf("skills: create system skills subdir: %w", err)
			}
			return nil
		}
		if parent, ok := target.Parent(); ok {
			if err := os.MkdirAll(parent.Path(), systemSkillsCacheDirPerms); err != nil {
				return fmt.Errorf("skills: create system skills file parent: %w", err)
			}
		}
		data, err := systemSkillsFS.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target.Path(), data, systemSkillsFilePerms); err != nil {
			return fmt.Errorf("skills: write system skill file: %w", err)
		}
		return nil
	})
}
