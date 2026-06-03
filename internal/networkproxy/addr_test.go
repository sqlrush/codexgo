package networkproxy

import (
	"net/netip"
	"testing"
)

func TestParseHostPort(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		defaultPort uint16
		wantHost    string
		wantPort    uint16
		wantErr     bool
	}{
		{"empty errors", "", 1234, "", 0, true},
		{"whitespace errors", "   ", 5555, "", 0, true},
		{"host port without scheme", "127.0.0.1:8080", 3128, "127.0.0.1", 8080, false},
		{"host port with scheme and path", "http://example.com:8080/some/path", 3128, "example.com", 8080, false},
		{"strips userinfo", "http://user:pass@host.example:5555", 3128, "host.example", 5555, false},
		{"ipv6 with brackets", "http://[::1]:9999", 3128, "::1", 9999, false},
		{"unbracketed ipv6 keeps default port", "2001:db8::1", 3128, "2001:db8::1", 3128, false},
		{"invalid port falls back to default", "example.com:notaport", 3128, "example.com", 3128, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseHostPort(tt.input, tt.defaultPort)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseHostPort(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.host != tt.wantHost || got.port != tt.wantPort {
				t.Errorf("parseHostPort(%q) = {%q, %d}, want {%q, %d}", tt.input, got.host, got.port, tt.wantHost, tt.wantPort)
			}
		})
	}
}

func TestResolveAddr(t *testing.T) {
	tests := []struct {
		input       string
		defaultPort uint16
		want        string
	}{
		{"localhost", 3128, "127.0.0.1:3128"},
		{"1.2.3.4", 80, "1.2.3.4:80"},
		{"http://[::1]:8080", 3128, "[::1]:8080"},
		{"http://example.com:5555", 3128, "127.0.0.1:5555"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := resolveAddr(tt.input, tt.defaultPort)
			if err != nil {
				t.Fatalf("resolveAddr(%q): %v", tt.input, err)
			}
			want := netip.MustParseAddrPort(tt.want)
			if got != want {
				t.Errorf("resolveAddr(%q) = %v, want %v", tt.input, got, want)
			}
		})
	}
}

func TestClampBindAddrs(t *testing.T) {
	httpIn := netip.MustParseAddrPort("0.0.0.0:3128")
	socksIn := netip.MustParseAddrPort("0.0.0.0:8081")

	t.Run("allows non-loopback when enabled", func(t *testing.T) {
		cfg := DefaultNetworkProxySettings()
		cfg.DangerouslyAllowNonLoopbackProxy = true
		http, socks := clampBindAddrs(httpIn, socksIn, cfg)
		if http != httpIn || socks != socksIn {
			t.Errorf("clamp = %v/%v, want unchanged", http, socks)
		}
	})

	t.Run("forces loopback when unix sockets enabled", func(t *testing.T) {
		cfg := DefaultNetworkProxySettings()
		cfg.DangerouslyAllowNonLoopbackProxy = true
		cfg = cfg.WithAllowUnixSockets([]string{"/tmp/docker.sock"})
		http, socks := clampBindAddrs(httpIn, socksIn, cfg)
		if http != netip.MustParseAddrPort("127.0.0.1:3128") {
			t.Errorf("http = %v, want loopback", http)
		}
		if socks != netip.MustParseAddrPort("127.0.0.1:8081") {
			t.Errorf("socks = %v, want loopback", socks)
		}
	})

	t.Run("forces loopback when all unix sockets enabled", func(t *testing.T) {
		cfg := DefaultNetworkProxySettings()
		cfg.DangerouslyAllowNonLoopbackProxy = true
		cfg.DangerouslyAllowAllUnixSockets = true
		http, socks := clampBindAddrs(httpIn, socksIn, cfg)
		if !http.Addr().IsLoopback() || !socks.Addr().IsLoopback() {
			t.Errorf("clamp = %v/%v, want loopback", http, socks)
		}
	})
}

func TestResolveRuntimeRejectsRelativeUnixSocket(t *testing.T) {
	cfg := NetworkProxyConfig{Network: DefaultNetworkProxySettings().WithAllowUnixSockets([]string{"relative.sock"})}
	if _, err := ResolveRuntime(cfg); err == nil {
		t.Fatal("expected relative unix socket to be rejected")
	}
}

func TestResolveRuntimeAcceptsUnixStyleAbsolute(t *testing.T) {
	cfg := NetworkProxyConfig{Network: DefaultNetworkProxySettings().WithAllowUnixSockets([]string{"/private/tmp/example.sock"})}
	if _, err := ResolveRuntime(cfg); err != nil {
		t.Fatalf("unix-style absolute path should be accepted: %v", err)
	}
}

func TestDefaultSettingsBaseline(t *testing.T) {
	got := DefaultNetworkProxySettings()
	if got.Enabled {
		t.Error("default should be disabled")
	}
	if !got.EnableSocks5 {
		t.Error("socks5 should default enabled")
	}
	if !got.AllowUpstreamProxy {
		t.Error("allow_upstream_proxy should default true")
	}
	if got.Mode != NetworkModeFull {
		t.Errorf("mode = %q, want full", got.Mode)
	}
	if got.AllowedDomains() != nil {
		t.Error("default should have no allowed domains (default-deny)")
	}
}

func TestHostAndPortFromNetworkAddr(t *testing.T) {
	tests := []struct {
		input       string
		defaultPort uint16
		want        string
	}{
		{"", 1234, "<missing>"},
		{"http://[::1]:8080", 3128, "[::1]:8080"},
	}
	for _, tt := range tests {
		if got := HostAndPortFromNetworkAddr(tt.input, tt.defaultPort); got != tt.want {
			t.Errorf("HostAndPortFromNetworkAddr(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
