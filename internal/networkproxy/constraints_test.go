package networkproxy

import "testing"

func boolPtr(b bool) *bool               { return &b }
func strSlicePtr(s []string) *[]string   { return &s }
func modePtr(m NetworkMode) *NetworkMode { return &m }

func enabledSettings(allowed, denied []string) NetworkProxySettings {
	s := settingsWithDomains(allowed, denied)
	s.Enabled = true
	return s
}

func TestValidatePolicyAgainstConstraints(t *testing.T) {
	tests := []struct {
		name        string
		config      NetworkProxyConfig
		constraints NetworkProxyConstraints
		wantErr     bool
	}{
		{
			name:        "disallows widening allowed domains",
			config:      NetworkProxyConfig{Network: enabledSettings([]string{"example.com", "evil.com"}, nil)},
			constraints: NetworkProxyConstraints{AllowedDomains: strSlicePtr([]string{"example.com"})},
			wantErr:     true,
		},
		{
			name:        "allows expanding when enabled",
			config:      NetworkProxyConfig{Network: enabledSettings([]string{"example.com", "api.openai.com"}, nil)},
			constraints: NetworkProxyConstraints{AllowedDomains: strSlicePtr([]string{"example.com"}), AllowlistExpansionEnabled: boolPtr(true)},
			wantErr:     false,
		},
		{
			name:        "disallows widening mode",
			config:      NetworkProxyConfig{Network: NetworkProxySettings{Enabled: true, Mode: NetworkModeFull}},
			constraints: NetworkProxyConstraints{Mode: modePtr(NetworkModeLimited)},
			wantErr:     true,
		},
		{
			name:        "allows narrowing wildcard allowlist",
			config:      NetworkProxyConfig{Network: enabledSettings([]string{"api.example.com"}, nil)},
			constraints: NetworkProxyConstraints{AllowedDomains: strSlicePtr([]string{"*.example.com"})},
			wantErr:     false,
		},
		{
			name:        "rejects widening wildcard allowlist",
			config:      NetworkProxyConfig{Network: enabledSettings([]string{"**.example.com"}, nil)},
			constraints: NetworkProxyConstraints{AllowedDomains: strSlicePtr([]string{"*.example.com"})},
			wantErr:     true,
		},
		{
			name:        "rejects global wildcard in managed allowlist",
			config:      NetworkProxyConfig{Network: enabledSettings([]string{"api.example.com"}, nil)},
			constraints: NetworkProxyConstraints{AllowedDomains: strSlicePtr([]string{"*"})},
			wantErr:     true,
		},
		{
			name:        "rejects bracketed global wildcard in managed allowlist",
			config:      NetworkProxyConfig{Network: enabledSettings([]string{"api.example.com"}, nil)},
			constraints: NetworkProxyConstraints{AllowedDomains: strSlicePtr([]string{"[*]"})},
			wantErr:     true,
		},
		{
			name:        "requires managed denied domains entries",
			config:      NetworkProxyConfig{Network: enabledSettings(nil, nil)},
			constraints: NetworkProxyConstraints{DeniedDomains: strSlicePtr([]string{"evil.com"})},
			wantErr:     true,
		},
		{
			name:        "rejects enabled when managed disables",
			config:      NetworkProxyConfig{Network: NetworkProxySettings{Enabled: true}},
			constraints: NetworkProxyConstraints{Enabled: boolPtr(false)},
			wantErr:     true,
		},
		{
			name:        "no constraints permits anything",
			config:      NetworkProxyConfig{Network: enabledSettings([]string{"anything.example"}, nil)},
			constraints: NetworkProxyConstraints{},
			wantErr:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePolicyAgainstConstraints(tt.config, tt.constraints)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePolicyAgainstConstraints err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSetNetworkModeRespectsConstraints(t *testing.T) {
	cfg := NetworkProxyConfig{Network: NetworkProxySettings{Enabled: true, Mode: NetworkModeLimited, EnableSocks5: true, AllowUpstreamProxy: true}}
	cs, err := BuildConfigState(cfg, NetworkProxyConstraints{Mode: modePtr(NetworkModeLimited)})
	if err != nil {
		t.Fatalf("BuildConfigState: %v", err)
	}
	state := NewNetworkProxyState(cs)
	if err := state.SetNetworkMode(NetworkModeFull); err == nil {
		t.Error("expected SetNetworkMode(full) to be rejected by limited constraint")
	}
	if state.NetworkMode() != NetworkModeLimited {
		t.Errorf("mode = %q, want limited (unchanged)", state.NetworkMode())
	}
}
