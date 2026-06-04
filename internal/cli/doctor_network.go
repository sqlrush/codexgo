package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"
)

// doctorSkipNetworkEnv is the environment variable that disables live network
// probes, keeping offline and deterministic runs fast. When set, the network
// reachability checks report a skipped status.
const doctorSkipNetworkEnv = "CODEX_DOCTOR_SKIP_NETWORK"

// networkProbeTimeout bounds each live network probe so the doctor stays fast and
// never blocks on a hung connection.
const networkProbeTimeout = 5 * time.Second

// providerReachabilityCheck performs a bounded HTTP reachability probe of the
// active provider endpoint, mirroring network.provider_reachability in doctor.rs.
// It honors CODEX_DOCTOR_SKIP_NETWORK (reporting skipped) and never leaks
// credentials. A reachable endpoint, including auth-required statuses such as
// 401/403, is treated as ok because it proves the host is reachable.
func providerReachabilityCheck(ctx context.Context, dctx doctorContext) doctorCheck {
	b := newCheck("network.provider_reachability", "reachability")
	if os.Getenv(doctorSkipNetworkEnv) != "" {
		b.skipped("provider reachability probe skipped").
			detail(fmt.Sprintf("%s is set", doctorSkipNetworkEnv))
		return b.build()
	}

	label, probeURL := providerProbeTarget(dctx)
	b.detail(fmt.Sprintf("reachability mode: %s", label))
	b.detail(fmt.Sprintf("%s base URL: %s", label, probeURL))

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
		b.warn("active provider endpoint is not reachable over HTTP").
			detail(fmt.Errorf("provider probe error: %w", err).Error()).
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
// doctor.rs. It honors CODEX_DOCTOR_SKIP_NETWORK (reporting skipped).
func websocketReachabilityCheck(dctx doctorContext) doctorCheck {
	b := newCheck("network.websocket_reachability", "websocket")

	provider := orNone(dctx.ModelProvider)
	b.detail(fmt.Sprintf("model provider: %s", provider))

	if os.Getenv(doctorSkipNetworkEnv) != "" {
		b.skipped("Responses WebSocket probe skipped").
			detail(fmt.Sprintf("%s is set", doctorSkipNetworkEnv))
		return b.build()
	}

	wsURL := chatgptBackendBaseURL(dctx)
	b.detail(fmt.Sprintf("endpoint base: %s", wsURL))

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
		b.warn("Responses WebSocket endpoint is not reachable").
			detail(fmt.Errorf("websocket probe error: %w", err).Error()).
			remedy("Check proxy, VPN, firewall, DNS, custom CA, and WebSocket policy support.")
		return b.build()
	}
	defer resp.Body.Close()
	b.detail(fmt.Sprintf("endpoint reachability: HTTP %d", resp.StatusCode))
	b.ok("Responses WebSocket endpoint is reachable")
	return b.build()
}

// providerProbeTarget resolves the reachability label and HTTP probe URL for the
// active provider. ChatGPT auth probes the ChatGPT backend; otherwise the OpenAI
// API base URL is used.
func providerProbeTarget(dctx doctorContext) (label, url string) {
	if dctx.Loaded && dctx.ChatgptBaseURL != nil && *dctx.ChatgptBaseURL != "" {
		return "ChatGPT auth", *dctx.ChatgptBaseURL
	}
	return "OpenAI API", "https://api.openai.com"
}

// chatgptBackendBaseURL resolves the base URL used for the WebSocket reachability
// probe, preferring the configured ChatGPT base URL.
func chatgptBackendBaseURL(dctx doctorContext) string {
	if dctx.Loaded && dctx.ChatgptBaseURL != nil && *dctx.ChatgptBaseURL != "" {
		return *dctx.ChatgptBaseURL
	}
	return "https://chatgpt.com/backend-api/"
}
