package sandbox

import (
	"runtime"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// rootEntry builds a Special(Root) entry with the given access mode.
func rootEntry(access protocol.FileSystemAccessMode) protocol.FileSystemSandboxEntry {
	return protocol.FileSystemSandboxEntry{
		Path:   protocol.NewFileSystemSpecialPath(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindRoot}),
		Access: access,
	}
}

// restricted builds a Restricted filesystem policy from the given entries.
func restricted(entries ...protocol.FileSystemSandboxEntry) protocol.FileSystemSandboxPolicy {
	return protocol.FileSystemSandboxPolicy{Kind: protocol.FileSystemSandboxKindRestricted, Entries: entries}
}

// unrestricted builds an Unrestricted filesystem policy.
func unrestricted() protocol.FileSystemSandboxPolicy {
	return protocol.FileSystemSandboxPolicy{Kind: protocol.FileSystemSandboxKindUnrestricted}
}

// externalSandboxFS builds an ExternalSandbox filesystem policy.
func externalSandboxFS() protocol.FileSystemSandboxPolicy {
	return protocol.FileSystemSandboxPolicy{Kind: protocol.FileSystemSandboxKindExternalSandbox}
}

func TestSandboxTypeAsMetricTag(t *testing.T) {
	cases := []struct {
		in   SandboxType
		want string
	}{
		{SandboxTypeNone, "none"},
		{SandboxTypeMacosSeatbelt, "seatbelt"},
		{SandboxTypeLinuxSeccomp, "seccomp"},
		{SandboxTypeWindowsRestrictedToken, "windows_sandbox"},
		{SandboxType(99), "none"},
	}
	for _, tc := range cases {
		if got := tc.in.AsMetricTag(); got != tc.want {
			t.Errorf("SandboxType(%d).AsMetricTag() = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestGetPlatformSandbox checks the platform default sandbox per OS, mirroring
// the per-platform platformSandbox arms.
func TestGetPlatformSandbox(t *testing.T) {
	t.Run("windows-disabled", func(t *testing.T) {
		st, ok := GetPlatformSandbox(false)
		switch runtime.GOOS {
		case "darwin":
			if !ok || st != SandboxTypeMacosSeatbelt {
				t.Fatalf("darwin: got (%v,%v), want (Seatbelt,true)", st, ok)
			}
		case "linux":
			if !ok || st != SandboxTypeLinuxSeccomp {
				t.Fatalf("linux: got (%v,%v), want (Seccomp,true)", st, ok)
			}
		case "windows":
			if ok || st != SandboxTypeNone {
				t.Fatalf("windows-disabled: got (%v,%v), want (None,false)", st, ok)
			}
		default:
			if ok || st != SandboxTypeNone {
				t.Fatalf("%s: got (%v,%v), want (None,false)", runtime.GOOS, st, ok)
			}
		}
	})

	t.Run("windows-enabled", func(t *testing.T) {
		st, ok := GetPlatformSandbox(true)
		switch runtime.GOOS {
		case "windows":
			if !ok || st != SandboxTypeWindowsRestrictedToken {
				t.Fatalf("windows-enabled: got (%v,%v), want (RestrictedToken,true)", st, ok)
			}
		case "darwin":
			if !ok || st != SandboxTypeMacosSeatbelt {
				t.Fatalf("darwin: got (%v,%v), want (Seatbelt,true)", st, ok)
			}
		case "linux":
			if !ok || st != SandboxTypeLinuxSeccomp {
				t.Fatalf("linux: got (%v,%v), want (Seccomp,true)", st, ok)
			}
		default:
			if ok || st != SandboxTypeNone {
				t.Fatalf("%s: got (%v,%v), want (None,false)", runtime.GOOS, st, ok)
			}
		}
	})
}

// TestShouldRequirePlatformSandbox ports the should_require_platform_sandbox
// behaviors from policy_transforms_tests.rs plus the structural arms (managed
// network requirements, external sandbox, unrestricted).
func TestShouldRequirePlatformSandbox(t *testing.T) {
	cases := []struct {
		name       string
		fs         protocol.FileSystemSandboxPolicy
		network    protocol.NetworkSandboxPolicy
		hasManaged bool
		want       bool
	}{
		{
			// managed network requirements always force a platform sandbox.
			name:       "managed-network-requirements-force-sandbox",
			fs:         unrestricted(),
			network:    protocol.NetworkSandboxPolicyEnabled,
			hasManaged: true,
			want:       true,
		},
		{
			// danger-full-access (unrestricted fs) with enabled network and no
			// managed requirements skips the platform sandbox.
			name:    "unrestricted-network-enabled-skips",
			fs:      unrestricted(),
			network: protocol.NetworkSandboxPolicyEnabled,
			want:    false,
		},
		{
			// full-access restricted policy (root write) skips the platform
			// sandbox when network is enabled.
			name:    "full-access-restricted-network-enabled-skips",
			fs:      restricted(rootEntry(protocol.FileSystemAccessModeWrite)),
			network: protocol.NetworkSandboxPolicyEnabled,
			want:    false,
		},
		{
			// full-access restricted policy still needs the sandbox when network
			// is restricted (filesystem is unrestricted but network is not).
			name:    "full-access-restricted-network-restricted-requires",
			fs:      restricted(rootEntry(protocol.FileSystemAccessModeWrite)),
			network: protocol.NetworkSandboxPolicyRestricted,
			want:    true,
		},
		{
			// root write with a deny carveout is no longer full-disk write, so
			// the platform sandbox is required even with network enabled.
			name: "root-write-with-deny-carveout-requires",
			fs: restricted(
				rootEntry(protocol.FileSystemAccessModeWrite),
				pathEntry("/blocked", protocol.FileSystemAccessModeDeny),
			),
			network: protocol.NetworkSandboxPolicyEnabled,
			want:    true,
		},
		{
			// external sandbox delegates enforcement; no platform sandbox when
			// network is restricted.
			name:    "external-sandbox-network-restricted-skips",
			fs:      externalSandboxFS(),
			network: protocol.NetworkSandboxPolicyRestricted,
			want:    false,
		},
		{
			// restricted read-only with network restricted requires a sandbox.
			name:    "restricted-read-only-network-restricted-requires",
			fs:      restricted(rootEntry(protocol.FileSystemAccessModeRead)),
			network: protocol.NetworkSandboxPolicyRestricted,
			want:    true,
		},
		{
			// unrestricted fs with restricted network requires a sandbox to
			// enforce the network restriction.
			name:    "unrestricted-network-restricted-requires",
			fs:      unrestricted(),
			network: protocol.NetworkSandboxPolicyRestricted,
			want:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ShouldRequirePlatformSandbox(tc.fs, tc.network, tc.hasManaged)
			if got != tc.want {
				t.Fatalf("ShouldRequirePlatformSandbox = %v, want %v", got, tc.want)
			}
		})
	}
}

// pathEntry builds an absolute-Path entry with the given access.
func pathEntry(p string, access protocol.FileSystemAccessMode) protocol.FileSystemSandboxEntry {
	return protocol.FileSystemSandboxEntry{
		Path:   protocol.NewFileSystemPath(protocol.AbsolutePath(p)),
		Access: access,
	}
}

// TestSelectInitial ports the manager_tests.rs select_initial matrix and adds
// the Forbid/Require preference arms.
func TestSelectInitial(t *testing.T) {
	platform, platformOK := GetPlatformSandbox(false)
	expectedPlatform := SandboxTypeNone
	if platformOK {
		expectedPlatform = platform
	}

	cases := []struct {
		name       string
		fs         protocol.FileSystemSandboxPolicy
		network    protocol.NetworkSandboxPolicy
		pref       SandboxablePreference
		winLevel   protocol.WindowsSandboxLevel
		hasManaged bool
		want       SandboxType
	}{
		{
			// danger-full-access defaults to no sandbox without network reqs.
			name:    "auto-unrestricted-network-enabled-none",
			fs:      unrestricted(),
			network: protocol.NetworkSandboxPolicyEnabled,
			pref:    SandboxablePreferenceAuto,
			want:    SandboxTypeNone,
		},
		{
			// danger-full-access uses platform sandbox with managed net reqs.
			name:       "auto-unrestricted-managed-net-platform",
			fs:         unrestricted(),
			network:    protocol.NetworkSandboxPolicyEnabled,
			pref:       SandboxablePreferenceAuto,
			hasManaged: true,
			want:       expectedPlatform,
		},
		{
			// restricted fs uses platform sandbox even without managed net reqs.
			name:    "auto-restricted-read-network-enabled-platform",
			fs:      restricted(rootEntry(protocol.FileSystemAccessModeRead)),
			network: protocol.NetworkSandboxPolicyEnabled,
			pref:    SandboxablePreferenceAuto,
			want:    expectedPlatform,
		},
		{
			// Forbid always yields no sandbox regardless of policy.
			name:    "forbid-restricted-none",
			fs:      restricted(rootEntry(protocol.FileSystemAccessModeRead)),
			network: protocol.NetworkSandboxPolicyRestricted,
			pref:    SandboxablePreferenceForbid,
			want:    SandboxTypeNone,
		},
		{
			// Require always selects the platform sandbox when one exists.
			name:    "require-unrestricted-platform",
			fs:      unrestricted(),
			network: protocol.NetworkSandboxPolicyEnabled,
			pref:    SandboxablePreferenceRequire,
			want:    expectedPlatform,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := selectInitial(tc.fs, tc.network, tc.pref, tc.winLevel, tc.hasManaged)
			if got != tc.want {
				t.Fatalf("selectInitial = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSelectInitialWindowsLevel verifies that the windowsSandboxLevel toggles
// the windowsSandboxEnabled flag passed to GetPlatformSandbox.
func TestSelectInitialWindowsLevel(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-sandbox-level only changes behavior on windows")
	}
	got := selectInitial(
		unrestricted(),
		protocol.NetworkSandboxPolicyEnabled,
		SandboxablePreferenceRequire,
		protocol.WindowsSandboxLevelRestrictedToken,
		false,
	)
	if got != SandboxTypeWindowsRestrictedToken {
		t.Fatalf("require with restricted-token level = %v, want WindowsRestrictedToken", got)
	}

	disabled := selectInitial(
		unrestricted(),
		protocol.NetworkSandboxPolicyEnabled,
		SandboxablePreferenceRequire,
		protocol.WindowsSandboxLevelDisabled,
		false,
	)
	if disabled != SandboxTypeNone {
		t.Fatalf("require with disabled level = %v, want None", disabled)
	}
}
