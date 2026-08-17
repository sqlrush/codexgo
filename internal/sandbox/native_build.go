package sandbox

import (
	"path"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// buildLinuxSandboxSpec resolves a SpawnRequest into the NativeSandboxSpec the
// Linux helper consumes. It reuses the shared policy-resolution helpers
// (getWritableRootsWithCwd, getReadableRootsWithCwd, ...) so the writable-root /
// protected-subpath / readable-root computation matches the macOS Seatbelt
// backend exactly. The result is immutable: callers receive a fresh value.
func buildLinuxSandboxSpec(req SpawnRequest) NativeSandboxSpec {
	fsPolicy := req.FileSystemSandboxPolicy
	cwd := req.policyCwd()

	fullWrite := fsHasFullDiskWriteAccess(fsPolicy)
	fullRead := fsHasFullDiskReadAccess(fsPolicy)

	var writableRoots []string
	var readOnlySubpaths []string
	var protectedSubpaths []string
	if !fullWrite {
		for _, root := range getWritableRootsWithCwd(fsPolicy, cwd) {
			rootPath := string(root.Root)
			writableRoots = append(writableRoots, rootPath)
			readOnlySubpaths = append(readOnlySubpaths, absolutePathsToStrings(root.ReadOnlySubpaths)...)
			for _, name := range root.ProtectedMetadataNames {
				protectedSubpaths = append(protectedSubpaths, path.Join(rootPath, name))
			}
		}
	}

	var readableRoots []string
	if !fullRead {
		readableRoots = getReadableRootsWithCwd(fsPolicy, cwd)
	}
	unreadableRoots := getUnreadableRootsWithCwd(fsPolicy, cwd)

	seccompMode := networkSeccompModeFor(
		req.NetworkSandboxPolicy,
		networkAllowsProxy(req),
		proxyRoutedNetwork(req),
	)

	return NativeSandboxSpec{
		Command:             append([]string(nil), req.Command...),
		Cwd:                 req.Cwd,
		FullDiskWriteAccess: fullWrite,
		FullDiskReadAccess:  fullRead,
		WritableRoots:       dedupPathsNoNormalize(writableRoots),
		ReadOnlySubpaths:    dedupPathsNoNormalize(readOnlySubpaths),
		ProtectedSubpaths:   dedupPathsNoNormalize(protectedSubpaths),
		ReadableRoots:       dedupPathsNoNormalize(readableRoots),
		UnreadableRoots:     dedupPathsNoNormalize(unreadableRoots),
		NetworkSeccompMode:  seccompMode,
	}
}

// networkAllowsProxy reports whether the command is permitted to reach a managed
// network proxy. Managed-network sessions (EnforceManagedNetwork) and any run
// with a live proxy keep the network seccomp filter fail-closed-but-proxy-open.
func networkAllowsProxy(req SpawnRequest) bool {
	return req.EnforceManagedNetwork || req.Network != nil
}

// proxyRoutedNetwork reports whether outbound traffic is routed through the local
// proxy bridge, which requires permitting AF_INET/AF_INET6 sockets in the
// isolated namespace.
func proxyRoutedNetwork(req SpawnRequest) bool {
	return req.Network != nil
}

// buildWindowsSandboxSpec resolves a SpawnRequest into the NativeSandboxSpec the
// Windows helper consumes. The Windows backend enforces filesystem restrictions
// via deny-read ACLs rather than namespaces, so it carries the resolved
// deny-read paths (unreadable roots and read-only protected subpaths) plus the
// requested enforcement tier.
func buildWindowsSandboxSpec(req SpawnRequest, level protocol.WindowsSandboxLevel) NativeSandboxSpec {
	spec := buildLinuxSandboxSpec(req)

	denyRead := make([]string, 0, len(spec.UnreadableRoots)+len(spec.ReadOnlySubpaths))
	denyRead = append(denyRead, spec.UnreadableRoots...)
	denyRead = append(denyRead, spec.ReadOnlySubpaths...)

	spec.DenyReadPaths = dedupPathsNoNormalize(denyRead)
	spec.WindowsSandboxLevel = level
	// The network seccomp mode is Linux-only; clear it for the Windows helper.
	spec.NetworkSeccompMode = NetworkSeccompModeNone
	return spec
}
