package sandbox

import "github.com/sqlrush/codexgo/pkg/protocol"

// globScanDepth mirrors the policy_transforms::GlobScanDepth enum: a bounded
// depth or an unbounded marker, used only when a deny-glob entry is present.
type globScanDepth struct {
	bounded   bool
	depth     uint
	unbounded bool
}

// mergeGlobScanMaxDepth mirrors policy_transforms::merge_glob_scan_max_depth.
// Depth participates only when the corresponding entry list carries a deny-glob
// entry; otherwise the side contributes nothing. Unbounded wins; otherwise the
// max of the two bounded depths is returned. A nil result means "absent".
func mergeGlobScanMaxDepth(
	leftEntries []protocol.FileSystemSandboxEntry,
	leftDepth *uint,
	rightEntries []protocol.FileSystemSandboxEntry,
	rightDepth *uint,
) *uint {
	left := effectiveGlobScanDepth(leftEntries, leftDepth)
	right := effectiveGlobScanDepth(rightEntries, rightDepth)

	switch {
	case (left != nil && left.unbounded) || (right != nil && right.unbounded):
		return nil
	case left != nil && left.bounded && right != nil && right.bounded:
		return uintPtr(max(left.depth, right.depth))
	case left != nil && left.bounded:
		return uintPtr(left.depth)
	case right != nil && right.bounded:
		return uintPtr(right.depth)
	default:
		return nil
	}
}

// effectiveGlobScanDepth mirrors policy_transforms::effective_glob_scan_depth:
// it returns a depth marker only when the entries contain a deny-read glob.
func effectiveGlobScanDepth(entries []protocol.FileSystemSandboxEntry, depth *uint) *globScanDepth {
	if !hasDenyGlobEntry(entries) {
		return nil
	}
	if depth != nil {
		return &globScanDepth{bounded: true, depth: *depth}
	}
	return &globScanDepth{unbounded: true}
}

// hasDenyGlobEntry reports whether entries hold any deny-access glob pattern.
func hasDenyGlobEntry(entries []protocol.FileSystemSandboxEntry) bool {
	for i := range entries {
		if entries[i].Access == protocol.FileSystemAccessModeDeny &&
			entries[i].Path.Type == protocol.FileSystemPathTypeGlobPattern {
			return true
		}
	}
	return false
}

func uintPtr(v uint) *uint { return &v }
