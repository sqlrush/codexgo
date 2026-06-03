package login

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// PkceCodes holds a PKCE verifier/challenge pair. Rust: pkce::PkceCodes.
type PkceCodes struct {
	CodeVerifier  string
	CodeChallenge string
}

// GeneratePKCE generates a PKCE verifier/challenge pair (S256). Mirrors
// pkce::generate_pkce: the verifier is URL-safe base64 (no padding) of 64 random
// bytes, and the challenge is URL-safe base64 (no padding) of SHA-256(verifier).
func GeneratePKCE() (PkceCodes, error) {
	var bytes [64]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return PkceCodes{}, fmt.Errorf("generate pkce bytes: %w", err)
	}
	codeVerifier := base64.RawURLEncoding.EncodeToString(bytes[:])
	digest := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(digest[:])
	return PkceCodes{
		CodeVerifier:  codeVerifier,
		CodeChallenge: codeChallenge,
	}, nil
}
