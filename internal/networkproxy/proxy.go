package networkproxy

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"runtime"
	"strings"
	"sync"
)

// Environment-variable keys. These match codex exactly; the env-var contract is
// drop-in-critical because child processes consume them to route traffic through
// the proxy.

// ProxyURLEnvKeys are the keys clients read to discover a proxy URL.
var ProxyURLEnvKeys = []string{
	"HTTP_PROXY",
	"HTTPS_PROXY",
	"WS_PROXY",
	"WSS_PROXY",
	"ALL_PROXY",
	"FTP_PROXY",
	"YARN_HTTP_PROXY",
	"YARN_HTTPS_PROXY",
	"NPM_CONFIG_HTTP_PROXY",
	"NPM_CONFIG_HTTPS_PROXY",
	"NPM_CONFIG_PROXY",
	"BUNDLE_HTTP_PROXY",
	"BUNDLE_HTTPS_PROXY",
	"PIP_PROXY",
	"DOCKER_HTTP_PROXY",
	"DOCKER_HTTPS_PROXY",
}

// AllProxyEnvKeys are the ALL_PROXY variants (used for SOCKS5).
var AllProxyEnvKeys = []string{"ALL_PROXY", "all_proxy"}

// ProxyActiveEnvKey marks that the managed proxy is active.
const ProxyActiveEnvKey = "CODEX_NETWORK_PROXY_ACTIVE"

// AllowLocalBindingEnvKey signals whether local binding is permitted.
const AllowLocalBindingEnvKey = "CODEX_NETWORK_ALLOW_LOCAL_BINDING"

const (
	electronGetUseProxyEnvKey = "ELECTRON_GET_USE_PROXY"
	nodeUseEnvProxyEnvKey     = "NODE_USE_ENV_PROXY"
	gitSSHCommandEnvKey       = "GIT_SSH_COMMAND"
)

// ProxyEnvKeys is the full set of keys the proxy may write (used to verify no
// unexpected keys are set).
var ProxyEnvKeys = []string{
	ProxyActiveEnvKey,
	AllowLocalBindingEnvKey,
	electronGetUseProxyEnvKey,
	nodeUseEnvProxyEnvKey,
	"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy",
	"YARN_HTTP_PROXY", "YARN_HTTPS_PROXY",
	"npm_config_http_proxy", "npm_config_https_proxy", "npm_config_proxy",
	"NPM_CONFIG_HTTP_PROXY", "NPM_CONFIG_HTTPS_PROXY", "NPM_CONFIG_PROXY",
	"BUNDLE_HTTP_PROXY", "BUNDLE_HTTPS_PROXY",
	"PIP_PROXY",
	"DOCKER_HTTP_PROXY", "DOCKER_HTTPS_PROXY",
	"WS_PROXY", "WSS_PROXY", "ws_proxy", "wss_proxy",
	"NO_PROXY", "no_proxy", "npm_config_noproxy", "NPM_CONFIG_NOPROXY",
	"YARN_NO_PROXY", "BUNDLE_NO_PROXY",
	"ALL_PROXY", "all_proxy",
	"FTP_PROXY", "ftp_proxy",
}

// NoProxyEnvKeys are the keys that hold the no-proxy list.
var NoProxyEnvKeys = []string{
	"NO_PROXY",
	"no_proxy",
	"npm_config_noproxy",
	"NPM_CONFIG_NOPROXY",
	"YARN_NO_PROXY",
	"BUNDLE_NO_PROXY",
}

// DefaultNoProxyValue keeps loopback and IP-literal private targets direct.
const DefaultNoProxyValue = "localhost,127.0.0.1,::1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"

// CodexProxyGitSSHCommandMarker is the macOS git-ssh marker prefix.
const CodexProxyGitSSHCommandMarker = "CODEX_PROXY_GIT_SSH_COMMAND=1 "

const (
	codexProxyGitSSHCommandPrefix = "CODEX_PROXY_GIT_SSH_COMMAND=1 ssh -o ProxyCommand='nc -X 5 -x "
	codexProxyGitSSHCommandSuffix = " %h %p'"
)

var (
	ftpProxyEnvKeys       = []string{"FTP_PROXY", "ftp_proxy"}
	websocketProxyEnvKeys = []string{"WS_PROXY", "WSS_PROXY", "ws_proxy", "wss_proxy"}
	httpProxyEnvKeys      = []string{
		"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy",
		"YARN_HTTP_PROXY", "YARN_HTTPS_PROXY",
		"npm_config_http_proxy", "npm_config_https_proxy", "npm_config_proxy",
		"NPM_CONFIG_HTTP_PROXY", "NPM_CONFIG_HTTPS_PROXY", "NPM_CONFIG_PROXY",
		"BUNDLE_HTTP_PROXY", "BUNDLE_HTTPS_PROXY",
		"PIP_PROXY",
		"DOCKER_HTTP_PROXY", "DOCKER_HTTPS_PROXY",
	}
)

// ProxyURLEnvValue returns the value for a canonical proxy key, falling back to
// its lowercase alias. Mirrors Rust's `proxy_url_env_value`.
func ProxyURLEnvValue(env map[string]string, canonicalKey string) (string, bool) {
	if value, ok := env[canonicalKey]; ok {
		return value, true
	}
	value, ok := env[strings.ToLower(canonicalKey)]
	return value, ok
}

// HasProxyURLEnvVars reports whether any proxy URL env var is set to a non-empty
// value. Mirrors Rust's `has_proxy_url_env_vars`.
func HasProxyURLEnvVars(env map[string]string) bool {
	for _, key := range ProxyURLEnvKeys {
		if value, ok := ProxyURLEnvValue(env, key); ok && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func setEnvKeys(env map[string]string, keys []string, value string) {
	for _, key := range keys {
		env[key] = value
	}
}

func codexProxyGitSSHCommand(socksAddr netip.AddrPort) string {
	return codexProxyGitSSHCommandPrefix + socksAddr.String() + codexProxyGitSSHCommandSuffix
}

func isCodexProxyGitSSHCommand(command string) bool {
	return strings.HasPrefix(command, codexProxyGitSSHCommandPrefix) &&
		strings.HasSuffix(command, codexProxyGitSSHCommandSuffix)
}

// ApplyProxyEnvOverrides writes the managed proxy env vars into env, overriding
// existing values so command-level env cannot bypass the proxy. Faithful port of
// Rust's `apply_proxy_env_overrides`. The macOS GIT_SSH_COMMAND handling is gated
// on runtime.GOOS == "darwin".
func ApplyProxyEnvOverrides(env map[string]string, httpAddr, socksAddr netip.AddrPort, socksEnabled, allowLocalBinding bool) {
	httpProxyURL := "http://" + httpAddr.String()
	socksProxyURL := "socks5h://" + socksAddr.String()

	env[ProxyActiveEnvKey] = "1"
	if allowLocalBinding {
		env[AllowLocalBindingEnvKey] = "1"
	} else {
		env[AllowLocalBindingEnvKey] = "0"
	}

	setEnvKeys(env, httpProxyEnvKeys, httpProxyURL)
	setEnvKeys(env, websocketProxyEnvKeys, httpProxyURL)
	setEnvKeys(env, NoProxyEnvKeys, DefaultNoProxyValue)

	env[electronGetUseProxyEnvKey] = "true"
	env[nodeUseEnvProxyEnvKey] = "1"

	if socksEnabled {
		setEnvKeys(env, AllProxyEnvKeys, socksProxyURL)
		setEnvKeys(env, ftpProxyEnvKeys, socksProxyURL)
	} else {
		setEnvKeys(env, AllProxyEnvKeys, httpProxyURL)
		setEnvKeys(env, ftpProxyEnvKeys, httpProxyURL)
	}

	if runtime.GOOS == "darwin" && socksEnabled {
		// Preserve existing non-Codex SSH wrappers, but refresh a previously
		// injected Codex fallback so it cannot point at a stale port.
		if existing, ok := env[gitSSHCommandEnvKey]; ok && !isCodexProxyGitSSHCommand(existing) {
			// Leave the user's wrapper untouched.
		} else {
			env[gitSSHCommandEnvKey] = codexProxyGitSSHCommand(socksAddr)
		}
	}
}

// NetworkProxyBuilder configures and builds a NetworkProxy.
type NetworkProxyBuilder struct {
	state          *NetworkProxyState
	httpAddr       *netip.AddrPort
	socksAddr      *netip.AddrPort
	managedByCodex bool
	decider        NetworkPolicyDecider
	observer       BlockedRequestObserver
}

// NewBuilder returns a builder with managed-by-codex defaulting to true.
func NewBuilder() *NetworkProxyBuilder {
	return &NetworkProxyBuilder{managedByCodex: true}
}

// State sets the (required) compiled policy state.
func (b *NetworkProxyBuilder) State(state *NetworkProxyState) *NetworkProxyBuilder {
	b.state = state
	return b
}

// HTTPAddr overrides the HTTP listener bind address.
func (b *NetworkProxyBuilder) HTTPAddr(addr netip.AddrPort) *NetworkProxyBuilder {
	b.httpAddr = &addr
	return b
}

// SocksAddr overrides the SOCKS5 listener bind address.
func (b *NetworkProxyBuilder) SocksAddr(addr netip.AddrPort) *NetworkProxyBuilder {
	b.socksAddr = &addr
	return b
}

// ManagedByCodex controls whether ephemeral loopback ports are reserved (true)
// or the configured ports are used (false).
func (b *NetworkProxyBuilder) ManagedByCodex(managed bool) *NetworkProxyBuilder {
	b.managedByCodex = managed
	return b
}

// PolicyDecider sets the policy decider that can override allowlist misses.
func (b *NetworkProxyBuilder) PolicyDecider(decider NetworkPolicyDecider) *NetworkProxyBuilder {
	b.decider = decider
	return b
}

// BlockedRequestObserver sets the blocked-request observer.
func (b *NetworkProxyBuilder) BlockedRequestObserver(observer BlockedRequestObserver) *NetworkProxyBuilder {
	b.observer = observer
	return b
}

// Build resolves bind addresses (reserving ephemeral loopback ports when managed)
// and returns a NetworkProxy ready to Run.
func (b *NetworkProxyBuilder) Build() (*NetworkProxy, error) {
	if b.state == nil {
		return nil, fmt.Errorf("NetworkProxyBuilder requires a state; supply one via Builder.State(...)")
	}
	if b.observer != nil {
		b.state.SetBlockedRequestObserver(b.observer)
	}
	currentCfg := b.state.CurrentConfig()

	var (
		httpAddr      netip.AddrPort
		socksAddr     netip.AddrPort
		httpListener  net.Listener
		socksListener net.Listener
	)

	runtimeCfg, err := ResolveRuntime(currentCfg)
	if err != nil {
		return nil, err
	}

	if b.managedByCodex {
		// Reserve ephemeral loopback ports so the proxy never collides with the
		// configured ports and is always loopback-only.
		httpListener, err = reserveLoopbackEphemeralListener()
		if err != nil {
			return nil, fmt.Errorf("reserve HTTP proxy listener: %w", err)
		}
		ap, ok := netipFromTCPAddr(httpListener.Addr())
		if !ok {
			httpListener.Close()
			return nil, fmt.Errorf("read reserved HTTP proxy address")
		}
		httpAddr = ap
		socksAddr = runtimeCfg.SocksAddr
		if currentCfg.Network.EnableSocks5 {
			socksListener, err = reserveLoopbackEphemeralListener()
			if err != nil {
				httpListener.Close()
				return nil, fmt.Errorf("reserve SOCKS5 proxy listener: %w", err)
			}
			if ap, ok := netipFromTCPAddr(socksListener.Addr()); ok {
				socksAddr = ap
			}
		}
	} else {
		httpAddr = runtimeCfg.HTTPAddr
		socksAddr = runtimeCfg.SocksAddr
		if b.httpAddr != nil {
			httpAddr = *b.httpAddr
		}
		if b.socksAddr != nil {
			socksAddr = *b.socksAddr
		}
	}

	// Reapply bind clamping for caller overrides so unix-socket proxying stays
	// loopback-only.
	httpAddr, socksAddr = clampBindAddrs(httpAddr, socksAddr, currentCfg.Network)

	return &NetworkProxy{
		state:         b.state,
		httpAddr:      httpAddr,
		socksAddr:     socksAddr,
		socksEnabled:  currentCfg.Network.EnableSocks5,
		enableUDP:     currentCfg.Network.EnableSocks5UDP,
		decider:       b.decider,
		httpListener:  httpListener,
		socksListener: socksListener,
		allowLocal:    currentCfg.Network.AllowLocalBinding,
	}, nil
}

func reserveLoopbackEphemeralListener() (net.Listener, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("bind loopback ephemeral port: %w", err)
	}
	return listener, nil
}

// NetworkProxy is a built, runnable proxy.
type NetworkProxy struct {
	state         *NetworkProxyState
	httpAddr      netip.AddrPort
	socksAddr     netip.AddrPort
	socksEnabled  bool
	enableUDP     bool
	decider       NetworkPolicyDecider
	httpListener  net.Listener
	socksListener net.Listener
	allowLocal    bool
}

// HTTPAddr returns the resolved HTTP listener address.
func (p *NetworkProxy) HTTPAddr() netip.AddrPort { return p.httpAddr }

// SocksAddr returns the resolved SOCKS5 listener address.
func (p *NetworkProxy) SocksAddr() netip.AddrPort { return p.socksAddr }

// State returns the underlying policy state.
func (p *NetworkProxy) State() *NetworkProxyState { return p.state }

// ApplyToEnv writes the managed proxy env vars into env, overriding existing
// values. Faithful port of Rust's `NetworkProxy::apply_to_env`.
func (p *NetworkProxy) ApplyToEnv(env map[string]string) {
	ApplyProxyEnvOverrides(env, p.httpAddr, p.socksAddr, p.socksEnabled, p.allowLocal)
}

// AddAllowedDomain adds a host to the allowlist on the running proxy.
func (p *NetworkProxy) AddAllowedDomain(host string) error { return p.state.AddAllowedDomain(host) }

// AddDeniedDomain adds a host to the denylist on the running proxy.
func (p *NetworkProxy) AddDeniedDomain(host string) error { return p.state.AddDeniedDomain(host) }

// NetworkProxyHandle controls a running proxy's lifecycle.
type NetworkProxyHandle struct {
	cancel  context.CancelFunc
	wg      *sync.WaitGroup
	errMu   sync.Mutex
	err     error
	stopped bool
}

// Run starts the proxy listeners. When the proxy is disabled, it returns a no-op
// handle. The provided context governs the proxy's lifetime; Shutdown also
// cancels it.
func (p *NetworkProxy) Run(ctx context.Context) (*NetworkProxyHandle, error) {
	if !p.state.Enabled() {
		// Disabled: no listeners. Release any reserved listeners.
		if p.httpListener != nil {
			p.httpListener.Close()
		}
		if p.socksListener != nil {
			p.socksListener.Close()
		}
		return &NetworkProxyHandle{cancel: func() {}, wg: &sync.WaitGroup{}, stopped: true}, nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	wg := &sync.WaitGroup{}
	handle := &NetworkProxyHandle{cancel: cancel, wg: wg}

	httpListener := p.httpListener
	if httpListener == nil {
		l, err := net.Listen("tcp", p.httpAddr.String())
		if err != nil {
			cancel()
			return nil, fmt.Errorf("bind HTTP proxy %s: %w", p.httpAddr, err)
		}
		httpListener = l
		if ap, ok := netipFromTCPAddr(l.Addr()); ok {
			p.httpAddr = ap
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := runHTTPProxy(runCtx, p.state, httpListener, p.decider); err != nil {
			handle.recordErr(err)
		}
	}()

	if p.socksEnabled {
		socksListener := p.socksListener
		if socksListener == nil {
			l, err := net.Listen("tcp", p.socksAddr.String())
			if err != nil {
				cancel()
				return nil, fmt.Errorf("bind SOCKS5 proxy %s: %w", p.socksAddr, err)
			}
			socksListener = l
			if ap, ok := netipFromTCPAddr(l.Addr()); ok {
				p.socksAddr = ap
			}
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := runSocks5(runCtx, p.state, socksListener, p.decider, p.enableUDP); err != nil {
				handle.recordErr(err)
			}
		}()
	} else if p.socksListener != nil {
		p.socksListener.Close()
	}

	return handle, nil
}

func (h *NetworkProxyHandle) recordErr(err error) {
	h.errMu.Lock()
	defer h.errMu.Unlock()
	if h.err == nil {
		h.err = err
	}
}

// Shutdown cancels the proxy and waits for listeners to stop.
func (h *NetworkProxyHandle) Shutdown() error {
	if h.stopped {
		return nil
	}
	h.cancel()
	h.wg.Wait()
	h.stopped = true
	h.errMu.Lock()
	defer h.errMu.Unlock()
	return h.err
}

// Wait blocks until the proxy listeners stop (e.g. via context cancellation).
func (h *NetworkProxyHandle) Wait() error {
	h.wg.Wait()
	h.errMu.Lock()
	defer h.errMu.Unlock()
	return h.err
}
