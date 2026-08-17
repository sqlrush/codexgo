package sandbox

import (
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func TestProcVersionIndicatesWSL1(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{"microsoft suffix", "Linux version 4.4.0-22621-Microsoft", true},
		{"wsl1 explicit", "Linux version 5.15.0-microsoft-standard-WSL1", true},
		{"wsl marker plus wsl1", "Linux version 5.15.0-wsl-microsoft-standard-WSL1", true},
		{"wsl2 standard", "Linux version 6.6.87.2-microsoft-standard-WSL2", false},
		{"wsl marker plus wsl2", "Linux version 6.6.87.2-wsl-microsoft-standard-WSL2", false},
		{"microsoft-standard wsl2-ish", "Linux version 4.19.104-microsoft-standard", false},
		{"wsl3 standard", "Linux version 6.6.87.2-microsoft-standard-WSL3", false},
		{"plain linux", "Linux version 6.8.0", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := procVersionIndicatesWSL1(tt.version); got != tt.want {
				t.Fatalf("procVersionIndicatesWSL1(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestNetworkSeccompModeFor(t *testing.T) {
	tests := []struct {
		name        string
		network     protocol.NetworkSandboxPolicy
		allowProxy  bool
		proxyRouted bool
		want        NetworkSeccompMode
	}{
		{"enabled no proxy -> none", protocol.NetworkSandboxPolicyEnabled, false, false, NetworkSeccompModeNone},
		{"enabled managed -> proxy routed", protocol.NetworkSandboxPolicyEnabled, true, true, NetworkSeccompModeProxyRouted},
		{"enabled managed not routed -> restricted", protocol.NetworkSandboxPolicyEnabled, true, false, NetworkSeccompModeRestricted},
		{"restricted -> restricted", protocol.NetworkSandboxPolicyRestricted, false, false, NetworkSeccompModeRestricted},
		{"restricted routed -> proxy routed", protocol.NetworkSandboxPolicyRestricted, false, true, NetworkSeccompModeProxyRouted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := networkSeccompModeFor(tt.network, tt.allowProxy, tt.proxyRouted)
			if got != tt.want {
				t.Fatalf("networkSeccompModeFor = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEncodeDecodeNativeSandboxSpec_RoundTrip(t *testing.T) {
	spec := NativeSandboxSpec{
		Command:             []string{"/bin/echo", "hello"},
		Cwd:                 "/work",
		FullDiskWriteAccess: false,
		FullDiskReadAccess:  true,
		WritableRoots:       []string{"/work", "/tmp"},
		ReadOnlySubpaths:    []string{"/work/secret"},
		ProtectedSubpaths:   []string{"/work/.git", "/work/.codex"},
		ReadableRoots:       []string{"/usr"},
		UnreadableRoots:     []string{"/etc/shadow"},
		NetworkSeccompMode:  NetworkSeccompModeRestricted,
		DenyReadPaths:       []string{"C:\\secrets"},
		WindowsSandboxLevel: protocol.WindowsSandboxLevelRestrictedToken,
	}

	encoded, err := EncodeNativeSandboxSpec(spec)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeNativeSandboxSpec(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(got, spec) {
		t.Fatalf("round trip mismatch:\n got=%+v\nwant=%+v", got, spec)
	}
}

func TestDecodeNativeSandboxSpec_Errors(t *testing.T) {
	if _, err := DecodeNativeSandboxSpec("not json"); err == nil {
		t.Fatal("expected error decoding invalid JSON")
	}
	if _, err := DecodeNativeSandboxSpec(`{"command":[]}`); err == nil {
		t.Fatal("expected error for empty command")
	}
}
