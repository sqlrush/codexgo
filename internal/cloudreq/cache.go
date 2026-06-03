// Package cloudreq is a faithful Go port of the dependency-light core of the
// Rust `codex-cloud-requirements` crate. It fetches a managed requirements.toml
// from the Codex backend with retry + timeout, caches the result in an
// HMAC-SHA256-signed cache file (30-minute TTL, 5-minute background refresh),
// and fails closed for eligible (Business/Enterprise) ChatGPT accounts.
//
// The cache file format and HMAC keys match the reference codex byte-for-byte so
// caches are interchangeable.
package cloudreq

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"time"
)

// Timeouts and intervals matching the reference codex constants.
const (
	// Timeout bounds a single fetch attempt sequence.
	Timeout = 15 * time.Second
	// MaxAttempts is the number of fetch attempts before failing closed.
	MaxAttempts = 5
	// CacheFilename is the cache file name in codex_home.
	CacheFilename = "cloud-requirements-cache.json"
	// CacheRefreshInterval is the background refresh cadence.
	CacheRefreshInterval = 5 * time.Minute
	// CacheTTL is how long a cached entry stays valid.
	CacheTTL = 30 * time.Minute
)

// Messages matching the reference codex strings exactly.
const (
	loadFailedMessage         = "Failed to load cloud requirements (workspace-managed policies)."
	parseFailedMessage        = "Cloud requirements (workspace-managed policies) are invalid and could not be parsed. Please contact your workspace admin."
	authRecoveryFailedMessage = "Your authentication session could not be refreshed automatically. Please log out and sign in again."
)

// cacheWriteHMACKey is the key used to sign new cache files. It must match the
// reference codex key exactly so caches round-trip.
var cacheWriteHMACKey = []byte("codex-cloud-requirements-cache-v3-064f8542-75b4-494c-a294-97d3ce597271")

// cacheReadHMACKeys is the set of keys accepted when verifying cache files.
var cacheReadHMACKeys = [][]byte{cacheWriteHMACKey}

// cacheFile is the on-disk cache representation. It mirrors the Rust
// `CloudRequirementsCacheFile`.
type cacheFile struct {
	SignedPayload cacheSignedPayload `json:"signed_payload"`
	Signature     string             `json:"signature"`
}

// cacheSignedPayload is the signed portion of the cache. It mirrors the Rust
// `CloudRequirementsCacheSignedPayload`. Field order matters: serde serializes
// in declaration order, and the HMAC is computed over that serialization.
type cacheSignedPayload struct {
	CachedAt      time.Time `json:"cached_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	ChatgptUserID *string   `json:"chatgpt_user_id"`
	AccountID     *string   `json:"account_id"`
	Contents      *string   `json:"contents"`
}

// cachePayloadBytes serializes the signed payload for signing/verification,
// mirroring the Rust `cache_payload_bytes` (serde_json::to_vec).
func cachePayloadBytes(payload cacheSignedPayload) ([]byte, error) {
	return json.Marshal(payload)
}

// signCachePayload returns a base64 HMAC-SHA256 signature, mirroring the Rust
// `sign_cache_payload`.
func signCachePayload(payloadBytes []byte) string {
	mac := hmac.New(sha256.New, cacheWriteHMACKey)
	mac.Write(payloadBytes)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// verifyCacheSignature verifies a base64 signature against any accepted key,
// mirroring the Rust `verify_cache_signature`.
func verifyCacheSignature(payloadBytes []byte, signature string) bool {
	sigBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false
	}
	for _, key := range cacheReadHMACKeys {
		mac := hmac.New(sha256.New, key)
		mac.Write(payloadBytes)
		if hmac.Equal(mac.Sum(nil), sigBytes) {
			return true
		}
	}
	return false
}
