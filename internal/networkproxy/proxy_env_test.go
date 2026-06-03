package networkproxy

import (
	"net/netip"
	"runtime"
	"testing"
)

func loopback(port uint16) netip.AddrPort {
	return netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), port)
}

func TestApplyProxyEnvOverridesSetsCommonToolVars(t *testing.T) {
	env := map[string]string{}
	ApplyProxyEnvOverrides(env, loopback(3128), loopback(8081), true, false)

	want := map[string]string{
		"HTTP_PROXY":              "http://127.0.0.1:3128",
		"HTTPS_PROXY":             "http://127.0.0.1:3128",
		"WS_PROXY":                "http://127.0.0.1:3128",
		"WSS_PROXY":               "http://127.0.0.1:3128",
		"npm_config_proxy":        "http://127.0.0.1:3128",
		"ALL_PROXY":               "socks5h://127.0.0.1:8081",
		"FTP_PROXY":               "socks5h://127.0.0.1:8081",
		"NO_PROXY":                DefaultNoProxyValue,
		ProxyActiveEnvKey:         "1",
		AllowLocalBindingEnvKey:   "0",
		electronGetUseProxyEnvKey: "true",
		nodeUseEnvProxyEnvKey:     "1",
	}
	for k, v := range want {
		if got := env[k]; got != v {
			t.Errorf("env[%q] = %q, want %q", k, got, v)
		}
	}

	noProxy := env["NO_PROXY"]
	for _, fragment := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
		if !contains(noProxy, fragment) {
			t.Errorf("NO_PROXY missing %q: %q", fragment, noProxy)
		}
	}
	if contains(noProxy, "169.254.0.0/16") {
		t.Errorf("NO_PROXY should not contain link-local block: %q", noProxy)
	}

	if runtime.GOOS == "darwin" {
		wantSSH := "CODEX_PROXY_GIT_SSH_COMMAND=1 ssh -o ProxyCommand='nc -X 5 -x 127.0.0.1:8081 %h %p'"
		if got := env[gitSSHCommandEnvKey]; got != wantSSH {
			t.Errorf("GIT_SSH_COMMAND = %q, want %q", got, wantSSH)
		}
	} else if _, ok := env[gitSSHCommandEnvKey]; ok {
		t.Errorf("GIT_SSH_COMMAND should not be set on %s", runtime.GOOS)
	}
}

func TestApplyProxyEnvOverridesSetsOnlyExpectedKeys(t *testing.T) {
	env := map[string]string{}
	ApplyProxyEnvOverrides(env, loopback(3128), loopback(8081), true, false)

	allowed := make(map[string]struct{}, len(ProxyEnvKeys))
	for _, k := range ProxyEnvKeys {
		allowed[k] = struct{}{}
	}
	for key := range env {
		if _, ok := allowed[key]; ok {
			continue
		}
		if runtime.GOOS == "darwin" && key == gitSSHCommandEnvKey {
			continue
		}
		t.Errorf("proxy env writer set unexpected key: %q", key)
	}
}

func TestApplyProxyEnvOverridesUsesHTTPForAllProxyWithoutSocks(t *testing.T) {
	env := map[string]string{}
	ApplyProxyEnvOverrides(env, loopback(3128), loopback(8081), false, true)

	if got := env["ALL_PROXY"]; got != "http://127.0.0.1:3128" {
		t.Errorf("ALL_PROXY = %q, want http://127.0.0.1:3128", got)
	}
	if got := env[AllowLocalBindingEnvKey]; got != "1" {
		t.Errorf("%s = %q, want 1", AllowLocalBindingEnvKey, got)
	}
}

func TestProxyURLEnvValueResolvesLowercaseAliases(t *testing.T) {
	env := map[string]string{"http_proxy": "http://127.0.0.1:3128"}
	got, ok := ProxyURLEnvValue(env, "HTTP_PROXY")
	if !ok || got != "http://127.0.0.1:3128" {
		t.Errorf("ProxyURLEnvValue = (%q, %v), want (http://127.0.0.1:3128, true)", got, ok)
	}
}

func TestHasProxyURLEnvVars(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"lowercase all_proxy", map[string]string{"all_proxy": "socks5h://127.0.0.1:8081"}, true},
		{"websocket key", map[string]string{"wss_proxy": "http://127.0.0.1:3128"}, true},
		{"empty value ignored", map[string]string{"HTTP_PROXY": "   "}, false},
		{"none", map[string]string{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasProxyURLEnvVars(tt.env); got != tt.want {
				t.Errorf("HasProxyURLEnvVars = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyProxyEnvOverridesPreservesUserGitSSHCommand(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("git-ssh marker is macOS-only")
	}
	env := map[string]string{
		gitSSHCommandEnvKey: "ssh -o ProxyCommand='tsh proxy ssh --cluster=dev %r@%h:%p'",
	}
	ApplyProxyEnvOverrides(env, loopback(3128), loopback(8081), true, false)
	if got := env[gitSSHCommandEnvKey]; got != "ssh -o ProxyCommand='tsh proxy ssh --cluster=dev %r@%h:%p'" {
		t.Errorf("GIT_SSH_COMMAND was overwritten: %q", got)
	}
}

func TestApplyProxyEnvOverridesRefreshesCodexGitSSHCommand(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("git-ssh marker is macOS-only")
	}
	env := map[string]string{
		gitSSHCommandEnvKey: codexProxyGitSSHCommand(loopback(8081)),
	}
	ApplyProxyEnvOverrides(env, loopback(43128), loopback(48081), true, false)
	want := codexProxyGitSSHCommand(loopback(48081))
	if got := env[gitSSHCommandEnvKey]; got != want {
		t.Errorf("GIT_SSH_COMMAND = %q, want refreshed %q", got, want)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
