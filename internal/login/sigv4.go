package login

import "context"

// SigV4Signer abstracts AWS SigV4 request signing for providers that require it
// (for example Amazon Bedrock). The login package only authenticates against
// OpenAI/ChatGPT endpoints, so it does not implement SigV4 itself; this small
// interface is the seam a Bedrock-backed provider would satisfy.
//
// This is intentionally a stub: there is no SigV4 implementation in the upstream
// codex-login crate either. A future Bedrock provider can supply an
// implementation without changing the login package.
type SigV4Signer interface {
	// Sign signs the request identified by method, url, and body, returning the
	// authorization headers to attach. Implementations must not mutate body.
	Sign(ctx context.Context, method, url string, body []byte) (map[string]string, error)
}
