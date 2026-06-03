package memories

import (
	"context"
	"strconv"
)

// list lists immediate entries under the requested path, mirroring local::list.
func (b *LocalBackend) list(_ context.Context, req ListRequest) (ListResponse, error) {
	maxResults := req.MaxResults
	if maxResults > MaxListResults {
		maxResults = MaxListResults
	}

	start, err := b.resolveScopedPath(req.Path)
	if err != nil {
		return ListResponse{}, err
	}

	startIndex, err := parseCursor(req.Cursor)
	if err != nil {
		return ListResponse{}, err
	}

	info, ok, err := metadataOrNone(start)
	if err != nil {
		return ListResponse{}, err
	}
	if !ok {
		return ListResponse{}, errNotFound(derefOr(req.Path, ""))
	}
	if symErr := rejectSymlink(displayRelativePath(b.root, start), info); symErr != nil {
		return ListResponse{}, symErr
	}

	var entries []MemoryEntry
	switch {
	case info.Mode().IsRegular():
		entries = []MemoryEntry{{
			Path:      displayRelativePath(b.root, start),
			EntryType: EntryFile,
		}}
	case info.IsDir():
		paths, dirErr := readSortedDirPaths(start)
		if dirErr != nil {
			return ListResponse{}, dirErr
		}
		for _, path := range paths {
			if isHiddenPath(path) {
				continue
			}
			childInfo, childOK, childErr := metadataOrNone(path)
			if childErr != nil {
				return ListResponse{}, childErr
			}
			if !childOK {
				continue
			}
			if childInfo.Mode()&modeSymlink != 0 {
				continue
			}
			var entryType MemoryEntryType
			switch {
			case childInfo.IsDir():
				entryType = EntryDirectory
			case childInfo.Mode().IsRegular():
				entryType = EntryFile
			default:
				continue
			}
			entries = append(entries, MemoryEntry{
				Path:      displayRelativePath(b.root, path),
				EntryType: entryType,
			})
		}
	}

	if startIndex > len(entries) {
		return ListResponse{}, errInvalidCursor(strconv.Itoa(startIndex), "exceeds result count")
	}

	endIndex := saturatingAdd(startIndex, maxResults)
	if endIndex > len(entries) {
		endIndex = len(entries)
	}
	var nextCursor *string
	if endIndex < len(entries) {
		cursor := strconv.Itoa(endIndex)
		nextCursor = &cursor
	}

	page := append([]MemoryEntry(nil), entries[startIndex:endIndex]...)
	return ListResponse{
		Path:       req.Path,
		Entries:    page,
		NextCursor: nextCursor,
		Truncated:  nextCursor != nil,
	}, nil
}
