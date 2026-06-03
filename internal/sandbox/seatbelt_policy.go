package sandbox

import (
	"sort"
	"strings"

	"github.com/sqlrush/codexgo/internal/networkproxy"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// CreateSeatbeltCommandArgsParams bundles the inputs for generating the
// sandbox-exec argument vector. Mirrors CreateSeatbeltCommandArgsParams.
type CreateSeatbeltCommandArgsParams struct {
	// Command is the program plus its arguments to run inside the sandbox.
	Command []string
	// FileSystemSandboxPolicy is the resolved filesystem policy.
	FileSystemSandboxPolicy protocol.FileSystemSandboxPolicy
	// NetworkSandboxPolicy is the resolved network policy.
	NetworkSandboxPolicy protocol.NetworkSandboxPolicy
	// SandboxPolicyCwd is the working directory the policy is resolved against.
	SandboxPolicyCwd string
	// EnforceManagedNetwork forces a restricted (proxy-routed) network policy
	// even when no proxy ports are present, failing closed when none exist.
	EnforceManagedNetwork bool
	// Network is the running network proxy, if any.
	Network *networkproxy.NetworkProxy
	// ExtraAllowUnixSockets lists additional absolute unix-socket paths to permit.
	ExtraAllowUnixSockets []string
}

// CreateSeatbeltCommandArgs builds the argument vector for /usr/bin/sandbox-exec
// (excluding the executable itself): ["-p", <policy>, "-D<key>=<path>"..., "--",
// <command>...]. The generated policy text matches codex byte-for-byte for a
// given policy. Mirrors create_seatbelt_command_args.
func CreateSeatbeltCommandArgs(args CreateSeatbeltCommandArgsParams) []string {
	policy, dirParams := buildSeatbeltPolicy(args)

	seatbeltArgs := make([]string, 0, 3+len(dirParams)+len(args.Command))
	seatbeltArgs = append(seatbeltArgs, "-p", policy)
	for _, param := range dirParams {
		seatbeltArgs = append(seatbeltArgs, "-D"+param.key+"="+param.path)
	}
	seatbeltArgs = append(seatbeltArgs, "--")
	seatbeltArgs = append(seatbeltArgs, args.Command...)
	return seatbeltArgs
}

// buildSeatbeltPolicy assembles the full .sbpl policy text and the accompanying
// -D parameter bindings. It is split out from CreateSeatbeltCommandArgs so the
// policy text can be golden-tested directly.
func buildSeatbeltPolicy(args CreateSeatbeltCommandArgsParams) (string, []dirParam) {
	fsPolicy := args.FileSystemSandboxPolicy
	cwd := args.SandboxPolicyCwd

	unreadableRoots := getUnreadableRootsWithCwd(fsPolicy, cwd)

	fileWritePolicy, fileWriteParams := buildFileWritePolicy(fsPolicy, cwd, unreadableRoots)
	fileReadPolicy, fileReadParams := buildFileReadPolicy(fsPolicy, cwd, unreadableRoots)

	proxy := buildProxyPolicyInputs(args.Network, args.ExtraAllowUnixSockets)
	networkPolicy := dynamicNetworkPolicyForNetwork(args.NetworkSandboxPolicy, args.EnforceManagedNetwork, proxy)

	includePlatformDefaults := fsIncludePlatformDefaults(fsPolicy)
	denyReadPolicy := buildSeatbeltUnreadableGlobPolicy(fsPolicy, cwd)

	sections := []string{
		macosSeatbeltBasePolicy,
		fileReadPolicy,
		fileWritePolicy,
		denyReadPolicy,
		networkPolicy,
	}
	if includePlatformDefaults {
		sections = append(sections, macosRestrictedReadOnlyPlatformDefaults)
	}

	fullPolicy := strings.Join(sections, "\n")

	dirParams := make([]dirParam, 0, len(fileReadParams)+len(fileWriteParams))
	dirParams = append(dirParams, fileReadParams...)
	dirParams = append(dirParams, fileWriteParams...)
	dirParams = append(dirParams, proxy.unixSocketDirParams()...)

	return fullPolicy, dirParams
}

// buildFileWritePolicy mirrors the file-write* branch of create_seatbelt_command_args.
func buildFileWritePolicy(fsPolicy protocol.FileSystemSandboxPolicy, cwd string, unreadableRoots []string) (string, []dirParam) {
	if fsHasFullDiskWriteAccess(fsPolicy) {
		if len(unreadableRoots) == 0 {
			// Allegedly more permissive than (allow file-write*).
			return `(allow file-write* (regex #"^/"))`, nil
		}
		return buildSeatbeltAccessPolicy("file-write*", "WRITABLE_ROOT", []seatbeltAccessRoot{
			{
				root:             filesystemRoot,
				excludedSubpaths: unreadableRoots,
			},
		})
	}

	writableRoots := getWritableRootsWithCwd(fsPolicy, cwd)
	roots := make([]seatbeltAccessRoot, 0, len(writableRoots))
	for _, root := range writableRoots {
		roots = append(roots, seatbeltAccessRoot{
			root:                   string(root.Root),
			excludedSubpaths:       absolutePathsToStrings(root.ReadOnlySubpaths),
			protectedMetadataNames: protectedMetadataNamesForSeatbeltWritableRoot(fsPolicy, root, cwd),
		})
	}
	return buildSeatbeltAccessPolicy("file-write*", "WRITABLE_ROOT", roots)
}

// buildFileReadPolicy mirrors the file-read* branch of create_seatbelt_command_args.
func buildFileReadPolicy(fsPolicy protocol.FileSystemSandboxPolicy, cwd string, unreadableRoots []string) (string, []dirParam) {
	const readHeader = "; allow read-only file operations\n"

	if fsHasFullDiskReadAccess(fsPolicy) {
		if len(unreadableRoots) == 0 {
			return readHeader + "(allow file-read*)", nil
		}
		policy, params := buildSeatbeltAccessPolicy("file-read*", "READABLE_ROOT", []seatbeltAccessRoot{
			{
				root:             filesystemRoot,
				excludedSubpaths: unreadableRoots,
			},
		})
		return readHeader + policy, params
	}

	readableRoots := getReadableRootsWithCwd(fsPolicy, cwd)
	roots := make([]seatbeltAccessRoot, 0, len(readableRoots))
	for _, root := range readableRoots {
		var excluded []string
		for _, unreadable := range unreadableRoots {
			if pathStartsWith(unreadable, root) {
				excluded = append(excluded, unreadable)
			}
		}
		roots = append(roots, seatbeltAccessRoot{
			root:             root,
			excludedSubpaths: excluded,
		})
	}
	policy, params := buildSeatbeltAccessPolicy("file-read*", "READABLE_ROOT", roots)
	if policy == "" {
		return "", params
	}
	return readHeader + policy, params
}

// filesystemRoot is the absolute filesystem root used for full-disk access roots.
// Mirrors root_absolute_path().
const filesystemRoot = "/"

// seatbeltAccessRoot mirrors SeatbeltAccessRoot.
type seatbeltAccessRoot struct {
	root                   string
	excludedSubpaths       []string
	protectedMetadataNames []string
}

// buildSeatbeltAccessPolicy mirrors build_seatbelt_access_policy: it emits an
// (allow <action> ...) block over the given roots together with the -D params
// that bind each root and excluded-subpath path. Returns ("", nil) when there
// are no roots.
func buildSeatbeltAccessPolicy(action, paramPrefix string, roots []seatbeltAccessRoot) (string, []dirParam) {
	var components []string
	var params []dirParam

	for index, accessRoot := range roots {
		root := accessRoot.root
		if normalized, ok := normalizePathForSandbox(root); ok {
			root = normalized
		}
		rootParam := paramPrefix + "_" + itoa(index)
		params = append(params, dirParam{key: rootParam, path: root})

		if len(accessRoot.excludedSubpaths) == 0 && len(accessRoot.protectedMetadataNames) == 0 {
			components = append(components, `(subpath (param "`+rootParam+`"))`)
			continue
		}

		requireParts := []string{`(subpath (param "` + rootParam + `"))`}
		for excludedIndex, excludedSubpath := range accessRoot.excludedSubpaths {
			if normalized, ok := normalizePathForSandbox(excludedSubpath); ok {
				excludedSubpath = normalized
			}
			excludedParam := paramPrefix + "_" + itoa(index) + "_EXCLUDED_" + itoa(excludedIndex)
			params = append(params, dirParam{key: excludedParam, path: excludedSubpath})
			// Exclude both the exact protected path and anything beneath it so a
			// fresh mkdir of the protected directory itself is also denied.
			requireParts = append(requireParts, `(require-not (literal (param "`+excludedParam+`")))`)
			requireParts = append(requireParts, `(require-not (subpath (param "`+excludedParam+`")))`)
		}
		for _, metadataName := range accessRoot.protectedMetadataNames {
			regex := strings.ReplaceAll(seatbeltProtectedMetadataNameRegex(root, metadataName), `"`, `\"`)
			requireParts = append(requireParts, `(require-not (regex #"`+regex+`"))`)
		}
		components = append(components, "(require-all "+strings.Join(requireParts, " ")+" )")
	}

	if len(components) == 0 {
		return "", nil
	}
	return "(allow " + action + "\n" + strings.Join(components, " ") + "\n)", params
}

// seatbeltProtectedMetadataNameRegex mirrors seatbelt_protected_metadata_name_regex.
func seatbeltProtectedMetadataNameRegex(root, name string) string {
	for len(root) > 1 && strings.HasSuffix(root, "/") {
		root = root[:len(root)-1]
	}
	escapedRoot := regexEscape(root)
	escapedName := regexEscape(name)
	if escapedRoot == "/" {
		return "^/" + escapedName + "(/.*)?$"
	}
	return "^" + escapedRoot + "/" + escapedName + "(/.*)?$"
}

// protectedMetadataNamesForSeatbeltWritableRoot mirrors
// protected_metadata_names_for_writable_root: it keeps the writable root's own
// protected names and appends any default protected metadata name that the
// policy would deny writing under the root.
func protectedMetadataNamesForSeatbeltWritableRoot(fsPolicy protocol.FileSystemSandboxPolicy, writableRoot protocol.WritableRoot, cwd string) []string {
	names := make([]string, len(writableRoot.ProtectedMetadataNames))
	copy(names, writableRoot.ProtectedMetadataNames)
	for _, name := range protectedMetadataPathNames {
		if containsString(names, name) {
			continue
		}
		metadataPath := joinPath(string(writableRoot.Root), name)
		if !canWritePathWithCwd(fsPolicy, metadataPath, cwd) {
			names = append(names, name)
		}
	}
	return names
}

// buildSeatbeltUnreadableGlobPolicy mirrors build_seatbelt_unreadable_glob_policy:
// it converts each unreadable glob into anchored regex deny rules applied to both
// reads and unlink-style writes.
func buildSeatbeltUnreadableGlobPolicy(fsPolicy protocol.FileSystemSandboxPolicy, cwd string) string {
	unreadableGlobs := getUnreadableGlobsWithCwd(fsPolicy, cwd)
	if len(unreadableGlobs) == 0 {
		return ""
	}

	var components []string
	for _, pattern := range unreadableGlobs {
		regexes := make(map[string]struct{})
		if regex, ok := seatbeltRegexForUnreadableGlob(pattern); ok {
			regexes[regex] = struct{}{}
		}
		if canonical, ok := canonicalizeGlobStaticPrefixForSandbox(pattern); ok {
			if regex, ok := seatbeltRegexForUnreadableGlob(canonical); ok {
				regexes[regex] = struct{}{}
			}
		}

		sorted := make([]string, 0, len(regexes))
		for regex := range regexes {
			sorted = append(sorted, regex)
		}
		sort.Strings(sorted)
		for _, regex := range sorted {
			escaped := strings.ReplaceAll(regex, `"`, `\"`)
			components = append(components, `(deny file-read* (regex #"`+escaped+`"))`)
			components = append(components, `(deny file-write-unlink (regex #"`+escaped+`"))`)
		}
	}

	return strings.Join(components, "\n")
}

// dynamicNetworkPolicyForNetwork mirrors dynamic_network_policy_for_network.
func dynamicNetworkPolicyForNetwork(networkPolicy protocol.NetworkSandboxPolicy, enforceManagedNetwork bool, proxy proxyPolicyInputs) string {
	hasSomeUnixSocketAccess := proxy.hasSomeUnixSocketAccess()
	// Only take the restricted-but-allow-specific-endpoints branch when there is
	// actually something to allow (proxy ports, local binding, or unix sockets).
	// When a proxy/managed network is required but exposes no usable endpoints we
	// must fall through to the fail-closed (empty) result below, not emit a policy.
	shouldUseRestricted := len(proxy.ports) > 0 ||
		proxy.allowLocalBinding ||
		(!networkPolicy.IsEnabled() && hasSomeUnixSocketAccess)

	if shouldUseRestricted {
		var b strings.Builder
		if proxy.allowLocalBinding {
			b.WriteString("; allow local binding and loopback traffic\n")
			b.WriteString("(allow network-bind (local ip \"*:*\"))\n")
			b.WriteString("(allow network-inbound (local ip \"localhost:*\"))\n")
			b.WriteString("(allow network-outbound (remote ip \"localhost:*\"))\n")
		}
		if proxy.allowLocalBinding && len(proxy.ports) > 0 {
			b.WriteString("; allow DNS lookups while application traffic remains proxy-routed\n")
			b.WriteString("(allow network-outbound (remote ip \"*:53\"))\n")
		}
		for _, port := range proxy.ports {
			b.WriteString("(allow network-outbound (remote ip \"localhost:" + uitoa(uint64(port)) + "\"))\n")
		}
		if udsPolicy := proxy.unixSocketPolicy(); udsPolicy != "" {
			b.WriteString("; allow unix domain sockets for local IPC\n")
			b.WriteString(udsPolicy)
		}
		return b.String() + macosSeatbeltNetworkPolicy
	}

	if proxy.hasProxyConfig {
		// Proxy configured but no usable loopback endpoints: fail closed.
		return ""
	}
	if enforceManagedNetwork {
		// Managed network required but no usable proxy endpoints: fail closed.
		return ""
	}

	if networkPolicy.IsEnabled() {
		var b strings.Builder
		b.WriteString("(allow network-outbound)\n(allow network-inbound)\n")
		if udsPolicy := proxy.unixSocketPolicy(); udsPolicy != "" {
			b.WriteString("; allow unix domain sockets for local IPC\n")
			b.WriteString(udsPolicy)
		}
		return b.String() + macosSeatbeltNetworkPolicy
	}
	return ""
}
