package agentidentity

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"runtime"
	"strings"
)

// AgentRegistrationURL returns the agent registration endpoint for the given
// ChatGPT base URL. Mirrors the Rust agent_registration_url.
func AgentRegistrationURL(chatgptBaseURL string) string {
	return strings.TrimRight(chatgptBaseURL, "/") + "/v1/agent/register"
}

// AgentTaskRegistrationURL returns the task registration endpoint for the given
// base URL and agent runtime id. Mirrors the Rust agent_task_registration_url.
func AgentTaskRegistrationURL(chatgptBaseURL, agentRuntimeID string) string {
	return fmt.Sprintf("%s/v1/agent/%s/task/register", strings.TrimRight(chatgptBaseURL, "/"), agentRuntimeID)
}

// AgentIdentityBiscuitURL returns the biscuit (authenticate_app_v2) endpoint.
// Mirrors the Rust agent_identity_biscuit_url.
func AgentIdentityBiscuitURL(chatgptBaseURL string) string {
	return strings.TrimRight(chatgptBaseURL, "/") + "/authenticate_app_v2"
}

// JWKSURL returns the agent-identity JWKS endpoint, choosing the wham-prefixed
// path for backend-api base URLs. Mirrors the Rust agent_identity_jwks_url.
func JWKSURL(chatgptBaseURL string) string {
	trimmed := strings.TrimRight(chatgptBaseURL, "/")
	if strings.Contains(trimmed, "/backend-api") {
		return trimmed + "/wham/agent-identities/jwks"
	}
	return trimmed + "/agent-identities/jwks"
}

// RequestID generates a unique agent-identity request id of the form
// "codex-agent-identity-<base64url(16 random bytes)>". Mirrors the Rust
// agent_identity_request_id.
func RequestID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("failed to generate agent identity request id: %w", err)
	}
	return "codex-agent-identity-" + base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// BillOfMaterials describes the agent build context attached to identity
// requests. Mirrors the Rust AgentBillOfMaterials (serde rename: snake_case
// field names match the struct field tags below).
type BillOfMaterials struct {
	AgentVersion    string `json:"agent_version"`
	AgentHarnessID  string `json:"agent_harness_id"`
	RunningLocation string `json:"running_location"`
}

// BuildBillOfMaterials assembles a BillOfMaterials from the agent version and a
// session source string. Mirrors the Rust build_abom: the harness id is
// "codex-app" for VS Code and "codex-cli" otherwise, and running_location is
// "{session_source}-{os}". The caller supplies the version and session source
// string because the Go port does not embed Cargo metadata.
func BuildBillOfMaterials(agentVersion, sessionSource string) BillOfMaterials {
	harness := "codex-cli"
	if sessionSource == "vscode" {
		harness = "codex-app"
	}
	return BillOfMaterials{
		AgentVersion:    agentVersion,
		AgentHarnessID:  harness,
		RunningLocation: fmt.Sprintf("%s-%s", sessionSource, rustOSName()),
	}
}

// rustOSName returns the operating-system name using Rust's
// std::env::consts::OS naming convention so that running_location matches the
// reference byte-for-byte. The only common divergence from runtime.GOOS is
// macOS ("darwin" in Go, "macos" in Rust); the remaining values agree.
func rustOSName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	default:
		return runtime.GOOS
	}
}
