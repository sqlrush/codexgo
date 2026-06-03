package memories

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// LocalBackend is the filesystem-backed Backend implementation. It serves the
// memory store from a directory tree rooted at root, mirroring
// LocalMemoriesBackend. The zero value is not usable; construct one with
// NewLocalBackend or FromCodexHome.
type LocalBackend struct {
	root string
}

// Ensure LocalBackend satisfies Backend.
var _ Backend = (*LocalBackend)(nil)

// FromCodexHome builds a LocalBackend rooted at codexHome/memories, mirroring
// LocalMemoriesBackend::from_codex_home.
func FromCodexHome(codexHome abspath.AbsolutePathBuf) *LocalBackend {
	return NewLocalBackend(codexHome.Join("memories").Path())
}

// NewLocalBackend builds a LocalBackend rooted at the given memory directory,
// mirroring LocalMemoriesBackend::from_memory_root.
func NewLocalBackend(root string) *LocalBackend {
	return &LocalBackend{root: root}
}

// Root returns the configured memory root directory.
func (b *LocalBackend) Root() string { return b.root }

// AddAdHocNote implements Backend.
func (b *LocalBackend) AddAdHocNote(ctx context.Context, req AddAdHocNoteRequest) (AddAdHocNoteResponse, error) {
	return b.addAdHocNote(ctx, req)
}

// List implements Backend.
func (b *LocalBackend) List(ctx context.Context, req ListRequest) (ListResponse, error) {
	return b.list(ctx, req)
}

// Read implements Backend.
func (b *LocalBackend) Read(ctx context.Context, req ReadRequest) (ReadResponse, error) {
	return b.read(ctx, req)
}

// Search implements Backend.
func (b *LocalBackend) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	return b.search(ctx, req)
}

// resolveScopedPath resolves a request-relative path against the root, enforcing
// the same access rules as resolve_scoped_path: reject parent/root/prefix
// traversal, treat hidden components as not-found, reject symlinks, and reject
// traversal through non-directory components. A nil relativePath returns the root.
func (b *LocalBackend) resolveScopedPath(relativePath *string) (string, error) {
	if relativePath == nil {
		return b.root, nil
	}
	rel := *relativePath
	components := splitComponents(rel)

	if hasTraversalComponent(rel) {
		return "", errInvalidPath(rel, "must stay within the memories root")
	}
	for _, component := range components {
		if isHiddenComponent(component) {
			return "", errNotFound(rel)
		}
	}

	scopedPath := b.root
	for idx, component := range components {
		scopedPath = filepath.Join(scopedPath, component)

		info, ok, err := metadataOrNone(scopedPath)
		if err != nil {
			return "", err
		}
		if !ok {
			for _, remaining := range components[idx+1:] {
				scopedPath = filepath.Join(scopedPath, remaining)
			}
			return scopedPath, nil
		}

		if err := rejectSymlink(displayRelativePath(b.root, scopedPath), info); err != nil {
			return "", err
		}
		if idx+1 < len(components) && !info.IsDir() {
			return "", errInvalidPath(rel, "traverses through a non-directory path component")
		}
	}
	return scopedPath, nil
}

// metadataOrNone returns the no-follow metadata for path. The ok flag is false
// when the path does not exist, mirroring metadata_or_none (which uses
// symlink_metadata).
func metadataOrNone(path string) (os.FileInfo, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, errIO(err)
	}
	return info, true, nil
}

// splitComponents splits a relative path into its components using either path
// separator, dropping empty and "." segments. ".." segments are preserved so
// hasTraversalComponent can reject them.
func splitComponents(rel string) []string {
	normalized := strings.ReplaceAll(rel, "\\", "/")
	parts := strings.Split(normalized, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		out = append(out, part)
	}
	return out
}

// hasTraversalComponent reports whether rel contains a parent-dir, root, or
// drive-prefix component, mirroring the ParentDir/RootDir/Prefix rejection in
// resolve_scoped_path.
func hasTraversalComponent(rel string) bool {
	normalized := strings.ReplaceAll(rel, "\\", "/")
	if strings.HasPrefix(normalized, "/") {
		return true
	}
	// Windows drive prefix such as "C:".
	if len(normalized) >= 2 && normalized[1] == ':' {
		return true
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}
