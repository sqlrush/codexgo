package login

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sqlrush/codexgo/internal/agentidentity"
)

// agentIdentityJWKSTimeout bounds the JWKS fetch. Mirrors
// AGENT_IDENTITY_JWKS_TIMEOUT in the agent-identity crate.
const agentIdentityJWKSTimeout = 10 * time.Second

// FetchAgentIdentityJWKS fetches and parses the agent-identity JWKS for the given
// ChatGPT base URL. Mirrors agent_identity::fetch_agent_identity_jwks.
func FetchAgentIdentityJWKS(ctx context.Context, httpClient *http.Client, chatgptBaseURL string) (*agentidentity.JWKSet, error) {
	reqCtx, cancel := context.WithTimeout(ctx, agentIdentityJWKSTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, agentidentity.JWKSURL(chatgptBaseURL), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to request agent identity JWKS: %w", err)
	}
	req.Header.Set("originator", originatorValue())

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request agent identity JWKS: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("agent identity JWKS endpoint returned an error: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to decode agent identity JWKS: %w", err)
	}
	return agentidentity.ParseJWKSet(body)
}

// VerifiedAgentIdentityRecord decodes the JWT (unverified) to confirm it parses,
// fetches the JWKS, verifies the JWT against it, and returns the derived record.
// Mirrors manager::verified_agent_identity_record.
func VerifiedAgentIdentityRecord(ctx context.Context, httpClient *http.Client, jwt, chatgptBaseURL string) (AgentIdentityAuthRecord, error) {
	// First confirm the unverified payload parses (matches the Rust call to
	// AgentIdentityAuthRecord::from_agent_identity_jwt before verification).
	if _, err := AgentIdentityAuthRecordFromJWT(jwt); err != nil {
		return AgentIdentityAuthRecord{}, err
	}
	jwks, err := FetchAgentIdentityJWKS(ctx, httpClient, chatgptBaseURL)
	if err != nil {
		return AgentIdentityAuthRecord{}, err
	}
	claims, err := agentidentity.VerifyJwtClaims(jwt, jwks)
	if err != nil {
		return AgentIdentityAuthRecord{}, err
	}
	return agentIdentityAuthRecordFromClaims(claims), nil
}
