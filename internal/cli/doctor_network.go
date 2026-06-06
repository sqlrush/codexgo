package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"time"

	"github.com/sqlrush/codexgo/internal/login"
	"github.com/sqlrush/codexgo/internal/modelproviderinfo"
)

// doctorSkipNetworkEnv is the environment variable that disables live network
// probes, keeping offline and deterministic runs fast. When set, the network
// reachability checks report a skipped status.
const doctorSkipNetworkEnv = "CODEXGO_DOCTOR_SKIP_NETWORK"

// networkProbeTimeout bounds each live network probe so the doctor stays fast and
// never blocks on a hung connection.
const networkProbeTimeout = 5 * time.Second

// providerReachabilityCheck performs a bounded HTTP reachability probe of the
// active provider endpoint, mirroring network.provider_reachability in doctor.rs.
// It honors CODEXGO_DOCTOR_SKIP_NETWORK (reporting skipped) and never leaks
// credentials. A reachable endpoint, including auth-required statuses such as
// 401/403, is treated as ok because it proves the host is reachable.
func providerReachabilityCheck(ctx context.Context, dctx doctorContext) doctorCheck {
	b := newCheck("network.provider_reachability", "reachability")
	if os.Getenv(doctorSkipNetworkEnv) != "" {
		b.skipped("provider reachability probe skipped").
			detail(fmt.Sprintf("%s is set", doctorSkipNetworkEnv))
		return b.build()
	}

	mode, label, probeURL := providerReachabilityPlan(dctx)
	b.detail(fmt.Sprintf("reachability mode: %s", mode))

	probeCtx, cancel := context.WithTimeout(ctx, networkProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodHead, probeURL, nil)
	if err != nil {
		b.warn("could not build provider probe request").
			detail(fmt.Errorf("request build error: %w", err).Error())
		return b.build()
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// One detail per endpoint, mirroring provider_reachability_check: a probe
		// failure renders the error inline with the base URL (no separate "base
		// URL" row), so the JSON key stays a single string.
		b.detail(fmt.Sprintf("%s base URL: %s provider probe error: %v (required)", label, probeURL, err)).
			warn("active provider endpoint is not reachable over HTTP").
			remedy("Check proxy, VPN, firewall, DNS, and custom CA configuration.")
		return b.build()
	}
	defer resp.Body.Close()
	b.detail(fmt.Sprintf("%s base URL: %s reachable (HTTP %d)", label, probeURL, resp.StatusCode))
	b.ok("active provider endpoints are reachable over HTTP")
	return b.build()
}

// websocketReachabilityCheck reports the active provider's WebSocket metadata and
// performs a bounded handshake probe, mirroring network.websocket_reachability in
// doctor.rs. It honors CODEXGO_DOCTOR_SKIP_NETWORK (reporting skipped).
func websocketReachabilityCheck(dctx doctorContext) doctorCheck {
	b := newCheck("network.websocket_reachability", "websocket")

	providerID := resolveModelProviderID(dctx.ModelProvider)
	provider := resolveProviderInfo(dctx, providerID)

	// Provider metadata rows mirror the leading details of
	// websocket_reachability_check in doctor.rs.
	b.detail(fmt.Sprintf("model provider: %s", providerID))
	b.detail(fmt.Sprintf("provider name: %s", provider.Name))
	b.detail(fmt.Sprintf("wire API: %s", string(provider.WireApi)))
	b.detail(fmt.Sprintf("supports websockets: %t", provider.SupportsWebsockets))
	pushProxyEnvDetail(b)

	if !provider.SupportsWebsockets {
		b.ok("Responses WebSocket is not enabled for the active provider")
		return b.build()
	}

	b.detail(fmt.Sprintf("connect timeout: %d ms", provider.WebsocketConnectTimeout().Milliseconds()))
	b.detail(fmt.Sprintf("auth mode: %s", websocketAuthModeName(dctx)))

	if os.Getenv(doctorSkipNetworkEnv) != "" {
		b.skipped("Responses WebSocket probe skipped").
			detail(fmt.Sprintf("%s is set", doctorSkipNetworkEnv))
		return b.build()
	}

	// codexgo has no Responses WebSocket handshake client, so the auth mode, DNS,
	// endpoint, and handshake-result rows codex emits from a live handshake are not
	// reproduced here; a bounded HTTPS HEAD probe of the backend stands in. See
	// DEVIATIONS.md (doctor).
	wsURL := chatgptBackendBaseURL(dctx)
	b.detail(fmt.Sprintf("endpoint: %s", wsURL))
	if dns := dnsAddressFamilyDetail(wsURL); dns != "" {
		b.detail(fmt.Sprintf("DNS: %s", dns))
	}

	ctx, cancel := context.WithTimeout(context.Background(), networkProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, wsURL, nil)
	if err != nil {
		b.warn("could not build WebSocket reachability request").
			detail(fmt.Errorf("request build error: %w", err).Error())
		return b.build()
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.warn("Responses WebSocket failed; HTTPS fallback may still work").
			detail(fmt.Errorf("handshake transport error: %w", err).Error()).
			remedy("Check proxy, VPN, firewall, DNS, custom CA, and WebSocket policy support.")
		return b.build()
	}
	defer resp.Body.Close()
	b.detail(fmt.Sprintf("handshake result: HTTP %d", resp.StatusCode))
	b.ok("Responses WebSocket endpoint is reachable")
	return b.build()
}

// resolveProviderInfo resolves the active provider's metadata from the config's
// model_providers map, falling back to the built-in provider registry, then to a
// minimal default carrying the provider id as its name. Mirrors how
// websocket_reachability_check reads config.model_provider.
func resolveProviderInfo(dctx doctorContext, providerID string) modelproviderinfo.ModelProviderInfo {
	if dctx.Loaded {
		if info, ok := dctx.Cfg.ModelProviders[providerID]; ok {
			return info
		}
	}
	if info, ok := modelproviderinfo.BuiltInModelProviders(dctx.ChatgptBaseURL)[providerID]; ok {
		return info
	}
	info := modelproviderinfo.DefaultModelProviderInfo()
	info.Name = providerID
	return info
}

// dnsAddressFamilyDetail resolves the endpoint host and summarizes the address
// families returned, mirroring dns_address_family_details in doctor.rs. It returns
// "" when the URL has no host so the DNS row is omitted (rather than misleading).
func dnsAddressFamilyDetail(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), networkProbeTimeout)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, parsed.Hostname())
	if err != nil {
		return fmt.Sprintf("lookup failed (%v)", err)
	}
	ipv4, ipv6 := 0, 0
	for _, addr := range addrs {
		if addr.IP.To4() != nil {
			ipv4++
		} else {
			ipv6++
		}
	}
	firstFamily := "none"
	if len(addrs) > 0 {
		if addrs[0].IP.To4() != nil {
			firstFamily = "IPv4"
		} else {
			firstFamily = "IPv6"
		}
	}
	return fmt.Sprintf("%d IPv4, %d IPv6, first %s", ipv4, ipv6, firstFamily)
}

// websocketAuthModeName resolves the auth-mode name for the WebSocket detail,
// mirroring auth_mode_name in doctor.rs (snake_case names; "none" when no auth is
// resolvable). codexgo derives it from auth env vars and stored auth rather than a
// live provider handshake. See DEVIATIONS.md (doctor).
func websocketAuthModeName(dctx doctorContext) string {
	if envVarPresent("OPENAI_API_KEY") || envVarPresent("CODEXGO_API_KEY") {
		return "api_key"
	}
	if envVarPresent("CODEXGO_ACCESS_TOKEN") {
		return "chatgpt"
	}
	if dctx.Loaded {
		if stored, err := login.LoadAuthDotJson(dctx.CodexHome, dctx.StoreMode); err == nil && stored != nil {
			switch {
			case stored.Tokens != nil:
				return "chatgpt"
			case stored.OpenAIAPIKey != nil:
				return "api_key"
			}
		}
	}
	return "none"
}

// pushProxyEnvDetail emits the proxy-env presence row shared by the network.env
// and websocket checks, mirroring push_proxy_env_details in doctor.rs.
func pushProxyEnvDetail(b *checkBuilder) {
	var present []string
	for _, name := range proxyEnvVars {
		if envVarPresent(name) {
			present = append(present, name)
		}
	}
	if len(present) == 0 {
		b.detail("proxy env vars: none")
	} else {
		sort.Strings(present)
		b.detail(fmt.Sprintf("proxy env vars present: %s", joinComma(present)))
	}
}

// providerReachabilityPlan resolves the reachability mode description, the active
// endpoint label, and the HTTP probe URL, mirroring provider_reachability_plan +
// provider_reachability_plan_from_parts in doctor.rs. The mode is derived from the
// provider's OpenAI-auth requirement, the auth env vars, and any stored auth:
//   - not required -> "provider auth", endpoint "<id> API" (provider base URL)
//   - API key      -> "API key auth", endpoint "<id> API" (provider base URL)
//   - ChatGPT      -> "ChatGPT auth", endpoint "ChatGPT" (chatgpt base URL)
func providerReachabilityPlan(dctx doctorContext) (mode, label, url string) {
	providerID := resolveModelProviderID(dctx.ModelProvider)
	provider := resolveProviderInfo(dctx, providerID)
	authMode := providerAuthReachabilityMode(dctx, provider.RequiresOpenAIAuth)

	switch authMode {
	case reachabilityModeAPIKey:
		base := "https://api.openai.com/v1"
		if provider.BaseURL != nil && *provider.BaseURL != "" {
			base = *provider.BaseURL
		}
		return "API key auth", fmt.Sprintf("%s API", providerID), base
	case reachabilityModeNotRequired:
		base := "https://api.openai.com/v1"
		if provider.BaseURL != nil && *provider.BaseURL != "" {
			base = *provider.BaseURL
		}
		return "provider auth", fmt.Sprintf("%s API", providerID), base
	default: // ChatGPT
		return "ChatGPT auth", "ChatGPT", chatgptBackendBaseURL(dctx)
	}
}

// reachabilityMode enumerates the provider-auth reachability modes, mirroring
// ProviderAuthReachabilityMode in doctor.rs.
type reachabilityMode int

const (
	reachabilityModeNotRequired reachabilityMode = iota
	reachabilityModeAPIKey
	reachabilityModeChatGPT
)

// providerAuthReachabilityMode resolves the reachability mode from the provider's
// OpenAI-auth requirement, auth env vars, and stored auth, mirroring
// provider_auth_reachability_mode_from_auth in doctor.rs.
func providerAuthReachabilityMode(dctx doctorContext, requiresOpenAIAuth bool) reachabilityMode {
	if !requiresOpenAIAuth {
		return reachabilityModeNotRequired
	}
	if envVarPresent("OPENAI_API_KEY") || envVarPresent("CODEXGO_API_KEY") {
		return reachabilityModeAPIKey
	}
	if envVarPresent("CODEXGO_ACCESS_TOKEN") {
		return reachabilityModeChatGPT
	}
	if dctx.Loaded {
		if stored, err := login.LoadAuthDotJson(dctx.CodexHome, dctx.StoreMode); err == nil && stored != nil {
			if stored.OpenAIAPIKey != nil && stored.Tokens == nil {
				return reachabilityModeAPIKey
			}
		}
	}
	return reachabilityModeChatGPT
}

// chatgptBackendBaseURL resolves the base URL used for the WebSocket reachability
// probe, preferring the configured ChatGPT base URL.
func chatgptBackendBaseURL(dctx doctorContext) string {
	if dctx.Loaded && dctx.ChatgptBaseURL != nil && *dctx.ChatgptBaseURL != "" {
		return *dctx.ChatgptBaseURL
	}
	return "https://chatgpt.com/backend-api/"
}
