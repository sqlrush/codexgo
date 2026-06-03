package networkproxy

import (
	"net/netip"
	"testing"
)

func TestNormalizeHost(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"lowercase and trim", "  ExAmPlE.CoM  ", "example.com"},
		{"strip port", "example.com:1234", "example.com"},
		{"preserve unbracketed ipv6", "2001:db8::1", "2001:db8::1"},
		{"strip trailing dot", "example.com.", "example.com"},
		{"strip trailing dot mixed case", "ExAmPlE.CoM.", "example.com"},
		{"strip trailing dot with port", "example.com.:443", "example.com"},
		{"strip brackets ipv6", "[::1]", "::1"},
		{"strip brackets ipv6 with port", "[::1]:443", "::1"},
		{"preserve scope id", "fe80::1%lo0", "fe80::1%lo0"},
		{"bracketed scope id", "[fe80::1%lo0]", "fe80::1%lo0"},
		{"percent-encoded scope id", "[fe80::1%25lo0]", "fe80::1%lo0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeHost(tt.input); got != tt.want {
				t.Errorf("NormalizeHost(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsLoopbackHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"localhost", true},
		{"localhost.", true},
		{"LOCALHOST", true},
		{"notlocalhost", false},
		{"127.0.0.1", true},
		{"::1", true},
		{"1.2.3.4", false},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			normalized, ok := parseHost(tt.host)
			if !ok {
				t.Fatalf("parseHost(%q) failed", tt.host)
			}
			if got := isLoopbackHost(normalized); got != tt.want {
				t.Errorf("isLoopbackHost(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestIsNonPublicIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"192.168.0.1", true},
		{"100.64.0.1", true},   // CGNAT
		{"192.0.0.1", true},    // protocol assignments
		{"192.0.2.1", true},    // TEST-NET-1
		{"198.18.0.1", true},   // benchmarking
		{"198.51.100.1", true}, // TEST-NET-2
		{"203.0.113.1", true},  // TEST-NET-3
		{"240.0.0.1", true},    // reserved
		{"0.1.2.3", true},      // this-network
		{"8.8.8.8", false},
		{"::ffff:127.0.0.1", true},
		{"::ffff:10.0.0.1", true},
		{"::ffff:8.8.8.8", false},
		{"::1", true},
		{"fe80::1", true},
		{"fc00::1", true},
		{"2606:4700:4700::1111", false}, // public IPv6
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			addr, err := netip.ParseAddr(tt.ip)
			if err != nil {
				t.Fatalf("ParseAddr(%q): %v", tt.ip, err)
			}
			if got := isNonPublicIP(addr); got != tt.want {
				t.Errorf("isNonPublicIP(%q) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}
