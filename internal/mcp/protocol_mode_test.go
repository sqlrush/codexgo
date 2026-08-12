package mcp

import "testing"

// TestProtocolModePreferredVersion ports the upstream
// protocol_modes_select_compatible_sdk_lifecycles assertions.
func TestProtocolModePreferredVersion(t *testing.T) {
	if got := ProtocolModeLegacy.PreferredProtocolVersion(); got != ProtocolVersion20250618 {
		t.Fatalf("legacy preferred = %q, want %q", got, ProtocolVersion20250618)
	}
	if got := ProtocolModeV20260728.PreferredProtocolVersion(); got != ProtocolVersion20260728 {
		t.Fatalf("modern preferred = %q, want %q", got, ProtocolVersion20260728)
	}
}

// TestNegotiateVersion covers the 3×matrix: client policy × server-selected version.
func TestNegotiateVersion(t *testing.T) {
	tests := []struct {
		name      string
		mode      ProtocolMode
		serverVer string
		want      string
		wantErr   bool
	}{
		{"legacy accepts legacy", ProtocolModeLegacy, "2025-06-18", "2025-06-18", false},
		{"legacy rejects modern", ProtocolModeLegacy, "2026-07-28", "", true},
		{"legacy empty→legacy fallback", ProtocolModeLegacy, "", "2025-06-18", false},
		// A server pinned to the first stable revision (2024-11-05) must still
		// connect: codexgo offers a newer version, the server answers with the one
		// it speaks, and the surface codexgo uses (tools, resources) is unchanged
		// across the two. Rejecting it broke real servers (gaussdb plugin).
		{"legacy accepts oldest revision", ProtocolModeLegacy, "2024-11-05", "2024-11-05", false},
		{"modern accepts modern", ProtocolModeV20260728, "2026-07-28", "2026-07-28", false},
		{"modern accepts legacy downgrade", ProtocolModeV20260728, "2025-06-18", "2025-06-18", false},
		{"modern accepts oldest revision", ProtocolModeV20260728, "2024-11-05", "2024-11-05", false},
		{"modern empty→legacy fallback", ProtocolModeV20260728, "", "2025-06-18", false},
		{"modern rejects unknown", ProtocolModeV20260728, "2099-01-01", "", true},
		{"legacy rejects unknown", ProtocolModeLegacy, "2099-01-01", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.mode.NegotiateVersion(tt.serverVer)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got %q", got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("NegotiateVersion(%q) = (%q, %v), want (%q, nil)",
					tt.serverVer, got, err, tt.want)
			}
		})
	}
}

// TestStdioMode ports stdio_requires_both_the_modern_feature_and_a_server_opt_in.
func TestStdioMode(t *testing.T) {
	tests := []struct {
		name      string
		mode      ProtocolMode
		requested string
		want      ProtocolMode
		wantErr   bool
	}{
		{"legacy stays legacy (no request)", ProtocolModeLegacy, "", ProtocolModeLegacy, false},
		{"legacy stays legacy (modern request ignored)", ProtocolModeLegacy, "2026-07-28", ProtocolModeLegacy, false},
		{"modern no request → legacy", ProtocolModeV20260728, "", ProtocolModeLegacy, false},
		{"modern + opt-in → modern", ProtocolModeV20260728, "2026-07-28", ProtocolModeV20260728, false},
		{"modern + bad version → error", ProtocolModeV20260728, "2020-01-01", ProtocolModeLegacy, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.mode.StdioMode(tt.requested)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error")
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("StdioMode(%q) = (%v, %v), want (%v, nil)", tt.requested, got, err, tt.want)
			}
		})
	}
}

// TestNegotiateServerVersion: codexgo-as-server echoes supported client version,
// else falls back to legacy default.
func TestNegotiateServerVersion(t *testing.T) {
	for _, tc := range []struct{ req, want string }{
		{"2026-07-28", "2026-07-28"},
		{"2025-06-18", "2025-06-18"},
		{"", "2025-06-18"},
		{"1999-01-01", "2025-06-18"},
	} {
		if got := NegotiateServerVersion(tc.req); got != tc.want {
			t.Fatalf("NegotiateServerVersion(%q) = %q, want %q", tc.req, got, tc.want)
		}
	}
}
