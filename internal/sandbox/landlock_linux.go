//go:build linux

package sandbox

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// landlockUnsupported reports that the running kernel lacks Landlock support so
// callers can degrade gracefully (namespaces + seccomp still apply).
var landlockUnsupported = errors.New("sandbox: landlock not supported by kernel")

// landlockAccessFSRead is the set of Landlock filesystem rights that constitute
// "read" access, used to grant read-only roots. Mirrors AccessFs::from_read in
// the Rust landlock crate (ABI v1-style read rights, intersected with what the
// kernel advertises).
const landlockAccessFSRead = unix.LANDLOCK_ACCESS_FS_EXECUTE |
	unix.LANDLOCK_ACCESS_FS_READ_FILE |
	unix.LANDLOCK_ACCESS_FS_READ_DIR

// landlockAccessFSWrite is the additional set of rights that constitute "write"
// access. Combined with landlockAccessFSRead it forms full read/write access
// (AccessFs::from_all).
const landlockAccessFSWrite = unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
	unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
	unix.LANDLOCK_ACCESS_FS_REMOVE_FILE |
	unix.LANDLOCK_ACCESS_FS_MAKE_CHAR |
	unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
	unix.LANDLOCK_ACCESS_FS_MAKE_REG |
	unix.LANDLOCK_ACCESS_FS_MAKE_SOCK |
	unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
	unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK |
	unix.LANDLOCK_ACCESS_FS_MAKE_SYM

// landlockAccessFSAll is the union of read and write rights.
const landlockAccessFSAll = landlockAccessFSRead | landlockAccessFSWrite

// landlockAccessFSFileOnly is the subset of rights that apply to non-directory
// targets (regular files, devices, fifos, sockets). A path_beneath rule whose
// target is not a directory must not carry directory-only rights, or
// landlock_add_rule returns EINVAL — so file targets are masked to this set.
// Mirrors the access-rights downgrade the Rust landlock crate performs for
// non-directory rule targets.
const landlockAccessFSFileOnly = unix.LANDLOCK_ACCESS_FS_EXECUTE |
	unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
	unix.LANDLOCK_ACCESS_FS_READ_FILE

// landlockABIVersion queries the kernel's supported Landlock ABI version via
// landlock_create_ruleset(NULL, 0, LANDLOCK_CREATE_RULESET_VERSION). A return
// value <= 0 means Landlock is unavailable.
func landlockABIVersion() (int, error) {
	ret, _, errno := unix.Syscall(
		unix.SYS_LANDLOCK_CREATE_RULESET,
		0,
		0,
		uintptr(unix.LANDLOCK_CREATE_RULESET_VERSION),
	)
	if errno != 0 {
		if errno == unix.ENOSYS || errno == unix.EOPNOTSUPP {
			return 0, landlockUnsupported
		}
		return 0, fmt.Errorf("sandbox: landlock_create_ruleset(version): %w", errno)
	}
	return int(ret), nil
}

// supportedLandlockAccessFS returns the subset of read/write rights actually
// available on the running kernel ABI. Newer rights (truncate, ioctl_dev, refer)
// are intentionally not requested so the ruleset stays best-effort compatible
// with older kernels, matching CompatLevel::BestEffort.
func supportedLandlockAccessFS(abi int) (read, all uint64) {
	if abi < 1 {
		return 0, 0
	}
	return landlockAccessFSRead, landlockAccessFSAll
}

// installFilesystemLandlock applies a Landlock ruleset on the current thread that
// grants read access to the whole filesystem, read/write to /dev/null and each
// writable root, and read-only to each protected/read-only subpath. It mirrors
// install_filesystem_landlock_rules_on_current_thread, extended to honor the
// protected-subpath carveouts the native backend computes.
//
// When the kernel lacks Landlock it returns landlockUnsupported so the caller can
// continue with namespace + seccomp enforcement only.
func installFilesystemLandlock(writableRoots, readOnlySubpaths []string) error {
	abi, err := landlockABIVersion()
	if err != nil {
		return err
	}
	accessRead, accessAll := supportedLandlockAccessFS(abi)
	if accessAll == 0 {
		return landlockUnsupported
	}

	attr := unix.LandlockRulesetAttr{Access_fs: accessAll}
	rulesetFD, err := landlockCreateRuleset(&attr)
	if err != nil {
		return err
	}
	defer unix.Close(rulesetFD)

	// Whole filesystem: read-only baseline.
	if err := addPathRule(rulesetFD, "/", accessRead); err != nil {
		return err
	}
	// /dev/null: read/write so tools can redirect to it under a RO root.
	if err := addPathRuleAllowMissing(rulesetFD, "/dev/null", accessAll); err != nil {
		return err
	}
	// Writable roots: full read/write.
	for _, root := range writableRoots {
		if err := addPathRuleAllowMissing(rulesetFD, root, accessAll); err != nil {
			return err
		}
	}
	// Read-only / protected subpaths: re-applied read-only so they win over the
	// writable root they sit under (Landlock takes the union of matching rules,
	// so a narrower read-only rule plus a broader rw rule still permits writes;
	// the namespace bind-mount layer is the authoritative protection. We add the
	// read-only rule for defense in depth and parity with the seatbelt policy).
	for _, sub := range readOnlySubpaths {
		if err := addPathRuleAllowMissing(rulesetFD, sub, accessRead); err != nil {
			return err
		}
	}

	if err := landlockRestrictSelf(rulesetFD); err != nil {
		return err
	}
	return nil
}

// landlockCreateRuleset wraps landlock_create_ruleset(attr, size, 0).
func landlockCreateRuleset(attr *unix.LandlockRulesetAttr) (int, error) {
	ret, _, errno := unix.Syscall(
		unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(attr)),
		unsafe.Sizeof(*attr),
		0,
	)
	if errno != 0 {
		return -1, fmt.Errorf("sandbox: landlock_create_ruleset: %w", errno)
	}
	return int(ret), nil
}

// addPathRule adds a path_beneath rule granting allowedAccess under path. It
// returns an error if path cannot be opened.
func addPathRule(rulesetFD int, path string, allowedAccess uint64) error {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("sandbox: landlock open %q: %w", path, err)
	}
	defer unix.Close(fd)

	// A path_beneath rule whose target is not a directory must carry only
	// file-applicable rights, or landlock_add_rule returns EINVAL. Detect the
	// target kind via fstat on the O_PATH fd and downgrade accordingly.
	if !isDirFD(fd) {
		allowedAccess &= landlockAccessFSFileOnly
	}

	beneath := unix.LandlockPathBeneathAttr{
		Allowed_access: allowedAccess,
		Parent_fd:      int32(fd),
	}
	_, _, errno := unix.Syscall6(
		unix.SYS_LANDLOCK_ADD_RULE,
		uintptr(rulesetFD),
		uintptr(unix.LANDLOCK_RULE_PATH_BENEATH),
		uintptr(unsafe.Pointer(&beneath)),
		0, 0, 0,
	)
	if errno != 0 {
		return fmt.Errorf("sandbox: landlock_add_rule %q: %w", path, errno)
	}
	return nil
}

// isDirFD reports whether the open file descriptor refers to a directory. A stat
// failure conservatively reports false (file rights only).
func isDirFD(fd int) bool {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return false
	}
	return st.Mode&unix.S_IFMT == unix.S_IFDIR
}

// addPathRuleAllowMissing is addPathRule but treats a missing path as a no-op,
// matching bubblewrap's "skip missing bind targets" behavior.
func addPathRuleAllowMissing(rulesetFD int, path string, allowedAccess uint64) error {
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("sandbox: landlock stat %q: %w", path, err)
	}
	return addPathRule(rulesetFD, path, allowedAccess)
}

// landlockRestrictSelf wraps landlock_restrict_self(ruleset_fd, 0). It requires
// PR_SET_NO_NEW_PRIVS to have been set first.
func landlockRestrictSelf(rulesetFD int) error {
	_, _, errno := unix.Syscall(
		unix.SYS_LANDLOCK_RESTRICT_SELF,
		uintptr(rulesetFD),
		0,
		0,
	)
	if errno != 0 {
		return fmt.Errorf("sandbox: landlock_restrict_self: %w", errno)
	}
	return nil
}
