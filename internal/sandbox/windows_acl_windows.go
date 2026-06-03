//go:build windows

package sandbox

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// denyReadMask is the access mask denied to the sandbox principal. It covers the
// generic read rights so a deny ACE blocks opening, reading, and listing the
// object. Mirrors the FILE_GENERIC_READ-style deny in acl.rs.
const denyReadMask = windows.GENERIC_READ | windows.GENERIC_EXECUTE

// applyDenyReadACLs applies a deny-read ACE for sid to each planned deny path,
// materializing missing paths as directories first so a sandboxed command cannot
// create a previously-absent denied path and then read it in the same run.
//
// It returns a revert function that removes the ACEs this call added, so a
// one-shot sandbox run does not leave persistent deny ACEs on the host. Mirrors
// apply_deny_read_acls in deny_read_acl.rs.
func applyDenyReadACLs(paths []string, sid *windows.SID) (revert func(), err error) {
	planned := planDenyReadACLPaths(paths, pathExists, canonicalizeWindowsPath)

	var added []string
	revertAll := func() {
		for _, p := range added {
			_ = revokeDenyReadACE(p, sid)
		}
	}

	for _, path := range planned {
		if !pathExists(path) {
			if mkErr := os.MkdirAll(path, 0o755); mkErr != nil {
				revertAll()
				return nil, fmt.Errorf("sandbox: create deny-read path %q: %w", path, mkErr)
			}
		}
		if aceErr := addDenyReadACE(path, sid); aceErr != nil {
			revertAll()
			return nil, fmt.Errorf("sandbox: apply deny-read ACE to %q: %w", path, aceErr)
		}
		added = append(added, path)
	}
	return revertAll, nil
}

// addDenyReadACE merges a DENY_ACCESS ACE for sid into the existing DACL of path.
func addDenyReadACE(path string, sid *windows.SID) error {
	existing := currentDACL(path)

	entry := windows.EXPLICIT_ACCESS{
		AccessPermissions: windows.ACCESS_MASK(denyReadMask),
		AccessMode:        windows.DENY_ACCESS,
		Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}

	merged, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{entry}, existing)
	if err != nil {
		return fmt.Errorf("build merged DACL for %q: %w", path, err)
	}

	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
		nil, nil, merged, nil,
	); err != nil {
		return fmt.Errorf("set DACL for %q: %w", path, err)
	}
	return nil
}

// revokeDenyReadACE rebuilds the DACL without the deny ACE for sid by re-applying
// a GRANT entry with a zero mask... but the simplest faithful revert is to set
// the DACL back to the entries that existed before. Since we merged into the
// existing DACL, we revoke by removing entries matching sid via a fresh DACL that
// re-grants nothing for sid. Here we conservatively reset the object to inherit
// from its parent, which drops the explicit deny ACE.
func revokeDenyReadACE(path string, _ *windows.SID) error {
	// Reset the DACL to unprotected so it re-inherits parent permissions, which
	// removes the explicit deny ACE this run added. This matches revoke_ace's
	// intent of leaving no persistent deny state after a one-shot run.
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, nil, nil,
	); err != nil {
		return fmt.Errorf("revoke DACL for %q: %w", path, err)
	}
	return nil
}

// currentDACL returns the existing DACL of path, or nil when it cannot be read
// (in which case ACLFromEntries starts from an empty DACL).
func currentDACL(path string) *windows.ACL {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return nil
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return nil
	}
	return dacl
}

// pathExists reports whether path exists on disk.
func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// canonicalizeWindowsPath resolves path to its canonical form (resolving any
// reparse points), falling back to the cleaned absolute path. Mirrors
// canonicalize_path's intent.
func canonicalizeWindowsPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}
