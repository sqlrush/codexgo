//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"sort"

	"golang.org/x/sys/unix"
)

// nsSetup describes the namespaces and mount overlay to establish before exec.
// It is derived from a NativeSandboxSpec.
type nsSetup struct {
	fullDiskWriteAccess bool
	fullDiskReadAccess  bool
	writableRoots       []string
	readOnlySubpaths    []string
	protectedSubpaths   []string
	readableRoots       []string
	unreadableRoots     []string
	unshareNet          bool
}

// nsSetupFromSpec projects a spec into the namespace setup. The network
// namespace is unshared whenever a network seccomp filter applies AND the
// network is not proxy-routed (proxy-routed runs need a usable loopback/host
// route to the bridge, so they keep the host network namespace and rely on the
// seccomp family gate instead).
func nsSetupFromSpec(spec NativeSandboxSpec) nsSetup {
	return nsSetup{
		fullDiskWriteAccess: spec.FullDiskWriteAccess,
		fullDiskReadAccess:  spec.FullDiskReadAccess,
		writableRoots:       spec.WritableRoots,
		readOnlySubpaths:    spec.ReadOnlySubpaths,
		protectedSubpaths:   spec.ProtectedSubpaths,
		readableRoots:       spec.ReadableRoots,
		unreadableRoots:     spec.UnreadableRoots,
		unshareNet:          spec.NetworkSeccompMode == NetworkSeccompModeRestricted,
	}
}

// applyMountOverlay establishes the filesystem view inside the new mount
// namespace: a read-only root (full read) or scoped read-only binds over an
// otherwise-restricted root, writable binds for each writable root, and
// read-only re-binds for protected/read-only subpaths so they win over the
// writable root they sit under. Mirrors create_filesystem_args (the bubblewrap
// arg builder) translated to direct mount(2) calls.
//
// Full read access uses `--ro-bind / /`; restricted read keeps the same
// read-only root then masks everything outside the readable roots. (Bubblewrap
// starts from `--tmpfs /`; a native tmpfs root would discard /bin, /lib, ...
// before the command can exec, so the native port instead remounts the real
// root read-only and masks non-readable top-level entries, which yields the
// same observable read restriction without losing the loader.)
func applyMountOverlay(s nsSetup) error {
	// Make all mounts private so our changes do not propagate to the host.
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("sandbox: make mounts private: %w", err)
	}

	if !s.fullDiskWriteAccess {
		if err := remountRootReadOnly(); err != nil {
			return err
		}
	}

	if !s.fullDiskReadAccess {
		if err := maskNonReadableTopLevel(s); err != nil {
			return err
		}
	}

	// Re-enable writes for each allowed root by bind-mounting it over itself
	// read/write. Process shallow-to-deep so nested writable carveouts under a
	// broader root are applied after the broader root.
	for _, root := range sortByDepth(existingPaths(s.writableRoots)) {
		if err := bindReadWrite(root); err != nil {
			return err
		}
	}

	// Re-apply protected + read-only subpaths read-only so they override the
	// writable root. Process deep-to-shallow is unnecessary; each is independent.
	for _, sub := range existingPaths(append(append([]string{}, s.protectedSubpaths...), s.readOnlySubpaths...)) {
		if err := bindReadOnly(sub); err != nil {
			return err
		}
	}

	// Mask unrelated unreadable roots by over-mounting an empty read-only tmpfs.
	for _, deny := range existingPaths(s.unreadableRoots) {
		if err := maskPath(deny); err != nil {
			return err
		}
	}
	return nil
}

// maskNonReadableTopLevel masks each top-level filesystem entry that is neither a
// readable root, a writable root, nor an ancestor of one, restricting reads to
// the approved roots while keeping the loader and approved paths visible. It is
// the native equivalent of bubblewrap's `--tmpfs /` + scoped `--ro-bind` mounts.
func maskNonReadableTopLevel(s nsSetup) error {
	allowed := append(append([]string{}, s.readableRoots...), s.writableRoots...)
	// A read of "/" granted explicitly means "broad read baseline": skip masking.
	for _, root := range allowed {
		if root == "/" {
			return nil
		}
	}

	entries, err := os.ReadDir("/")
	if err != nil {
		return fmt.Errorf("sandbox: read / for restricted-read masking: %w", err)
	}
	for _, entry := range entries {
		top := "/" + entry.Name()
		if topLevelIsAllowed(top, allowed) {
			continue
		}
		if err := maskPath(top); err != nil {
			return err
		}
	}
	return nil
}

// topLevelIsAllowed reports whether a top-level path must stay readable because
// it is, or contains, or is contained by, an allowed root.
func topLevelIsAllowed(top string, allowed []string) bool {
	for _, root := range allowed {
		if root == "" {
			continue
		}
		if pathStartsWith(root, top) || pathStartsWith(top, root) {
			return true
		}
	}
	return false
}

// remountRootReadOnly bind-mounts / over itself and remounts it read-only, the
// native equivalent of bubblewrap's `--ro-bind / /`.
func remountRootReadOnly() error {
	if err := unix.Mount("/", "/", "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("sandbox: bind / for read-only remount: %w", err)
	}
	const roFlags = unix.MS_BIND | unix.MS_REMOUNT | unix.MS_RDONLY | unix.MS_REC
	if err := unix.Mount("", "/", "", roFlags, ""); err != nil {
		return fmt.Errorf("sandbox: remount / read-only: %w", err)
	}
	return nil
}

// bindReadWrite bind-mounts path over itself read/write so writes are permitted
// even under a read-only root. A fresh bind mount inherits the read-only
// attribute of the underlying (already read-only) root, so an explicit
// MS_REMOUNT|MS_BIND without MS_RDONLY is issued to clear it.
func bindReadWrite(path string) error {
	if err := unix.Mount(path, path, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("sandbox: bind writable root %q: %w", path, err)
	}
	const rwFlags = unix.MS_BIND | unix.MS_REMOUNT | unix.MS_REC
	if err := unix.Mount("", path, "", rwFlags, ""); err != nil {
		return fmt.Errorf("sandbox: remount writable root %q read-write: %w", path, err)
	}
	return nil
}

// bindReadOnly bind-mounts path over itself and remounts it read-only.
func bindReadOnly(path string) error {
	if err := unix.Mount(path, path, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("sandbox: bind read-only subpath %q: %w", path, err)
	}
	const roFlags = unix.MS_BIND | unix.MS_REMOUNT | unix.MS_RDONLY | unix.MS_REC
	if err := unix.Mount("", path, "", roFlags, ""); err != nil {
		return fmt.Errorf("sandbox: remount read-only subpath %q: %w", path, err)
	}
	return nil
}

// maskPath over-mounts an empty read-only tmpfs at path so its contents are
// hidden. Directories are masked with tmpfs; files are masked by bind-mounting
// /dev/null over them.
func maskPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("sandbox: stat masked path %q: %w", path, err)
	}
	if info.IsDir() {
		if err := unix.Mount("tmpfs", path, "tmpfs", unix.MS_RDONLY|unix.MS_NOSUID, ""); err != nil {
			return fmt.Errorf("sandbox: mask directory %q: %w", path, err)
		}
		return nil
	}
	if err := unix.Mount("/dev/null", path, "", unix.MS_BIND, ""); err != nil {
		return fmt.Errorf("sandbox: mask file %q: %w", path, err)
	}
	return nil
}

// mountFreshProc mounts a fresh /proc reflecting the new PID namespace. It must
// run after the PID namespace is active (i.e. in the forked child). It returns
// false (with no error) when a fresh /proc cannot be mounted, which happens in
// restrictive containers that deny new proc mounts; callers then keep the
// inherited /proc. Mirrors the mount_proc preflight in linux_run_main.rs.
func mountFreshProc() (bool, error) {
	if err := unix.Mount("proc", "/proc", "proc", unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, ""); err != nil {
		if isContainerProcMountFailure(err) {
			return false, nil
		}
		return false, fmt.Errorf("sandbox: mount fresh /proc: %w", err)
	}
	return true, nil
}

// isContainerProcMountFailure reports whether a /proc mount error is the kind
// restrictive containers raise (EPERM/EACCES/EINVAL), which the sandbox treats
// as "skip fresh /proc" rather than a hard failure.
func isContainerProcMountFailure(err error) bool {
	switch err {
	case unix.EPERM, unix.EACCES, unix.EINVAL, unix.EBUSY:
		return true
	default:
		return false
	}
}

// isRunningInContainer heuristically detects container environments where PID
// namespace + fresh /proc handling must be relaxed. It checks the standard
// container markers. This is advisory only; mount failures are still handled
// gracefully by mountFreshProc.
func isRunningInContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return true
	}
	return false
}

// existingPaths returns the subset of paths that currently exist, deduplicated
// while preserving order.
func existingPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		if _, err := os.Lstat(p); err != nil {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// sortByDepth returns paths ordered shallow-to-deep (by component count), so a
// broader bind is applied before its nested descendants. Mirrors the
// sort_by_key(path_depth) calls in the bwrap arg builder.
func sortByDepth(paths []string) []string {
	out := append([]string(nil), paths...)
	sort.SliceStable(out, func(i, j int) bool {
		return componentCount(out[i]) < componentCount(out[j])
	})
	return out
}
