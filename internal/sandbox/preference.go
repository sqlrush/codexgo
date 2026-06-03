package sandbox

import "github.com/sqlrush/codexgo/internal/protocol"

// SandboxType identifies which platform sandbox backend a command should run
// under. Mirrors codex-sandboxing SandboxType.
type SandboxType int

const (
	// SandboxTypeNone runs the command with no outer sandbox.
	SandboxTypeNone SandboxType = iota
	// SandboxTypeMacosSeatbelt runs the command under macOS Seatbelt.
	SandboxTypeMacosSeatbelt
	// SandboxTypeLinuxSeccomp runs the command under the Linux seccomp/Landlock
	// sandbox (see spec 13).
	SandboxTypeLinuxSeccomp
	// SandboxTypeWindowsRestrictedToken runs the command under the Windows
	// restricted-token sandbox (see spec 14).
	SandboxTypeWindowsRestrictedToken
)

// AsMetricTag returns the metric tag for the sandbox type, mirroring
// SandboxType::as_metric_tag.
func (s SandboxType) AsMetricTag() string {
	switch s {
	case SandboxTypeNone:
		return "none"
	case SandboxTypeMacosSeatbelt:
		return "seatbelt"
	case SandboxTypeLinuxSeccomp:
		return "seccomp"
	case SandboxTypeWindowsRestrictedToken:
		return "windows_sandbox"
	default:
		return "none"
	}
}

// SandboxablePreference expresses how strongly a command wants to run under a
// platform sandbox. Mirrors codex-sandboxing SandboxablePreference.
type SandboxablePreference int

const (
	// SandboxablePreferenceAuto lets the policy decide whether to sandbox.
	SandboxablePreferenceAuto SandboxablePreference = iota
	// SandboxablePreferenceRequire always selects the platform sandbox when one
	// is available.
	SandboxablePreferenceRequire
	// SandboxablePreferenceForbid always runs without a platform sandbox.
	SandboxablePreferenceForbid
)

// GetPlatformSandbox returns the platform's default sandbox type, mirroring
// get_platform_sandbox. On Windows the restricted-token sandbox is only returned
// when windowsSandboxEnabled is true; unsupported platforms return (None,false).
func GetPlatformSandbox(windowsSandboxEnabled bool) (SandboxType, bool) {
	return platformSandbox(windowsSandboxEnabled)
}

// ShouldRequirePlatformSandbox mirrors should_require_platform_sandbox: it
// reports whether the effective filesystem and network policies (plus any
// managed-network requirements) demand an outer platform sandbox.
func ShouldRequirePlatformSandbox(
	fileSystemPolicy protocol.FileSystemSandboxPolicy,
	networkPolicy protocol.NetworkSandboxPolicy,
	hasManagedNetworkRequirements bool,
) bool {
	if hasManagedNetworkRequirements {
		return true
	}

	if !networkPolicy.IsEnabled() {
		return fileSystemPolicy.Kind != protocol.FileSystemSandboxKindExternalSandbox
	}

	switch fileSystemPolicy.Kind {
	case protocol.FileSystemSandboxKindRestricted:
		return !fsHasFullDiskWriteAccess(fileSystemPolicy)
	default: // Unrestricted, ExternalSandbox
		return false
	}
}

// EffectivePermissionProfile mirrors policy_transforms::effective_permission_profile:
// it folds an optional AdditionalPermissionProfile overlay into the base profile
// and returns the resulting profile with the base enforcement preserved.
func EffectivePermissionProfile(
	base protocol.PermissionProfile,
	additional *protocol.AdditionalPermissionProfile,
) protocol.PermissionProfile {
	fsPolicy, networkPolicy := profileRuntimePermissions(base)
	effectiveFS := effectiveFileSystemSandboxPolicy(fsPolicy, additional)
	effectiveNet := effectiveNetworkSandboxPolicy(networkPolicy, additional)
	return profileFromRuntimeWithEnforcement(base.Enforcement(), effectiveFS, effectiveNet)
}

// selectInitial mirrors SandboxManager::select_initial.
func selectInitial(
	fileSystemPolicy protocol.FileSystemSandboxPolicy,
	networkPolicy protocol.NetworkSandboxPolicy,
	pref SandboxablePreference,
	windowsSandboxLevel protocol.WindowsSandboxLevel,
	hasManagedNetworkRequirements bool,
) SandboxType {
	switch pref {
	case SandboxablePreferenceForbid:
		return SandboxTypeNone
	case SandboxablePreferenceRequire:
		if st, ok := GetPlatformSandbox(windowsSandboxLevel != protocol.WindowsSandboxLevelDisabled); ok {
			return st
		}
		return SandboxTypeNone
	default: // Auto
		if ShouldRequirePlatformSandbox(fileSystemPolicy, networkPolicy, hasManagedNetworkRequirements) {
			if st, ok := GetPlatformSandbox(windowsSandboxLevel != protocol.WindowsSandboxLevelDisabled); ok {
				return st
			}
		}
		return SandboxTypeNone
	}
}
