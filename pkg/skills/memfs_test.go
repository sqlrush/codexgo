package skills

import (
	"context"
	"os"
	"sort"
	"strings"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// memFS is an in-memory FileSystem for hermetic tests. Keys are normalized
// absolute path strings.
type memFS struct {
	files map[string]string
	dirs  map[string]struct{}
	links map[string]struct{}
}

func newMemFS() *memFS {
	return &memFS{
		files: make(map[string]string),
		dirs:  make(map[string]struct{}),
		links: make(map[string]struct{}),
	}
}

// addFile registers a file at the given absolute path, creating ancestor dirs.
func (m *memFS) addFile(path, contents string) {
	abs := mustAbs(path)
	m.files[abs.String()] = contents
	m.ensureParents(abs)
}

// addDir registers an empty directory and its ancestors.
func (m *memFS) addDir(path string) {
	abs := mustAbs(path)
	m.dirs[abs.String()] = struct{}{}
	m.ensureParents(abs)
}

// addSymlink registers a symlink directory entry (treated as a directory whose
// contents come from listing target paths recorded separately).
func (m *memFS) addSymlinkDir(path string) {
	abs := mustAbs(path)
	m.links[abs.String()] = struct{}{}
	m.dirs[abs.String()] = struct{}{}
	m.ensureParents(abs)
}

func (m *memFS) ensureParents(p abspath.AbsolutePathBuf) {
	cur := p
	for {
		parent, ok := cur.Parent()
		if !ok {
			return
		}
		m.dirs[parent.String()] = struct{}{}
		cur = parent
	}
}

func (m *memFS) GetMetadata(_ context.Context, path abspath.AbsolutePathBuf) (FileMetadata, error) {
	key := path.String()
	if _, ok := m.links[key]; ok {
		return FileMetadata{IsSymlink: true, IsDirectory: false, IsFile: false}, nil
	}
	if _, ok := m.files[key]; ok {
		return FileMetadata{IsFile: true}, nil
	}
	if _, ok := m.dirs[key]; ok {
		return FileMetadata{IsDirectory: true}, nil
	}
	return FileMetadata{}, os.ErrNotExist
}

func (m *memFS) ReadFileText(_ context.Context, path abspath.AbsolutePathBuf) (string, error) {
	if contents, ok := m.files[path.String()]; ok {
		return contents, nil
	}
	return "", os.ErrNotExist
}

func (m *memFS) ReadDirectory(_ context.Context, dir abspath.AbsolutePathBuf) ([]DirEntry, error) {
	prefix := dir.String()
	if prefix != "/" {
		prefix += "/"
	}
	seen := make(map[string]struct{})
	var names []string
	collect := func(key string) {
		if !strings.HasPrefix(key, prefix) {
			return
		}
		rest := key[len(prefix):]
		if rest == "" {
			return
		}
		name := rest
		if idx := strings.IndexByte(rest, '/'); idx >= 0 {
			name = rest[:idx]
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for key := range m.files {
		collect(key)
	}
	for key := range m.dirs {
		collect(key)
	}
	for key := range m.links {
		collect(key)
	}
	if _, ok := m.dirs[dir.String()]; !ok {
		return nil, os.ErrNotExist
	}
	sort.Strings(names)
	out := make([]DirEntry, 0, len(names))
	for _, name := range names {
		out = append(out, DirEntry{FileName: name})
	}
	return out, nil
}

func mustAbs(path string) abspath.AbsolutePathBuf {
	abs, err := abspath.FromAbsolutePathChecked(path)
	if err != nil {
		panic(err)
	}
	return abs
}
