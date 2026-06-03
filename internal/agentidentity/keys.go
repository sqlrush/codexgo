package agentidentity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"time"
)

// Key is the stored key material for a registered agent identity. It mirrors
// the Rust AgentIdentityKey, holding the agent runtime id and the PKCS#8
// (DER) Ed25519 private key encoded as standard base64.
type Key struct {
	AgentRuntimeID        string
	PrivateKeyPKCS8Base64 string
}

// TaskAuthorizationTarget is the task binding used when constructing a
// task-scoped AgentAssertion. Rust: AgentTaskAuthorizationTarget.
type TaskAuthorizationTarget struct {
	AgentRuntimeID string
	TaskID         string
}

// GeneratedKeyMaterial holds a freshly generated Ed25519 keypair: the PKCS#8
// private key (standard base64) and the OpenSSH-format public key.
type GeneratedKeyMaterial struct {
	PrivateKeyPKCS8Base64 string
	PublicKeySSH          string
}

// unixTime converts a unix timestamp (seconds) to a time.Time in UTC.
func unixTime(secs int64) time.Time { return time.Unix(secs, 0).UTC() }

// signingKeyFromPrivateKeyPKCS8Base64 decodes the stored standard-base64 PKCS#8
// DER and returns the Ed25519 private key. Mirrors the Rust helper of the same
// name (base64 decode, then PKCS#8 parse).
func signingKeyFromPrivateKeyPKCS8Base64(b64 string) (ed25519.PrivateKey, error) {
	der, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("stored agent identity private key is not valid base64: %w", err)
	}
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("stored agent identity private key is not valid PKCS#8: %w", err)
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("stored agent identity private key is not Ed25519")
	}
	return priv, nil
}

// GenerateKeyMaterial generates a new Ed25519 keypair, returning the PKCS#8
// private key as standard base64 and the OpenSSH-format public key. Mirrors the
// Rust generate_agent_key_material.
func GenerateKeyMaterial() (GeneratedKeyMaterial, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return GeneratedKeyMaterial{}, fmt.Errorf("failed to generate agent identity private key: %w", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return GeneratedKeyMaterial{}, fmt.Errorf("failed to encode agent identity private key as PKCS#8: %w", err)
	}
	return GeneratedKeyMaterial{
		PrivateKeyPKCS8Base64: base64.StdEncoding.EncodeToString(der),
		PublicKeySSH:          encodeSSHEd25519PublicKey(pub),
	}, nil
}

// PublicKeySSHFromPrivateKeyPKCS8Base64 derives the OpenSSH public key string
// from a stored PKCS#8 private key. Mirrors the Rust helper of the same name.
func PublicKeySSHFromPrivateKeyPKCS8Base64(b64 string) (string, error) {
	priv, err := signingKeyFromPrivateKeyPKCS8Base64(b64)
	if err != nil {
		return "", err
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return "", fmt.Errorf("agent identity private key has no Ed25519 public key")
	}
	return encodeSSHEd25519PublicKey(pub), nil
}

// SignTaskRegistrationPayload signs the "{agent_runtime_id}:{timestamp}" payload
// with the stored Ed25519 key, returning a standard-base64 signature. Mirrors
// the Rust sign_task_registration_payload.
func SignTaskRegistrationPayload(key Key, timestamp string) (string, error) {
	priv, err := signingKeyFromPrivateKeyPKCS8Base64(key.PrivateKeyPKCS8Base64)
	if err != nil {
		return "", err
	}
	payload := key.AgentRuntimeID + ":" + timestamp
	sig := ed25519.Sign(priv, []byte(payload))
	return base64.StdEncoding.EncodeToString(sig), nil
}

// signAgentAssertionPayload signs the
// "{agent_runtime_id}:{task_id}:{timestamp}" payload, returning a
// standard-base64 signature. Mirrors the Rust sign_agent_assertion_payload.
func signAgentAssertionPayload(key Key, taskID, timestamp string) (string, error) {
	priv, err := signingKeyFromPrivateKeyPKCS8Base64(key.PrivateKeyPKCS8Base64)
	if err != nil {
		return "", err
	}
	payload := key.AgentRuntimeID + ":" + taskID + ":" + timestamp
	sig := ed25519.Sign(priv, []byte(payload))
	return base64.StdEncoding.EncodeToString(sig), nil
}

// curve25519SecretKeyFromSigningKey derives the X25519 (Curve25519) secret key
// from an Ed25519 signing key by clamping the first 32 bytes of SHA-512 of the
// seed, exactly as crypto_box does in the Rust implementation. The returned
// slice is the 32-byte clamped scalar.
func curve25519SecretKeyFromSigningKey(priv ed25519.PrivateKey) []byte {
	seed := priv.Seed()
	digest := sha512.Sum512(seed)
	secret := make([]byte, 32)
	copy(secret, digest[:32])
	secret[0] &= 248
	secret[31] &= 127
	secret[31] |= 64
	return secret
}

// Curve25519SecretKeyFromPrivateKeyPKCS8Base64 returns the clamped Curve25519
// secret scalar derived from the stored Ed25519 private key. Mirrors the Rust
// curve25519_secret_key_from_private_key_pkcs8_base64. (The crypto_box unseal
// operation that consumes it is left to the registration client; see register.go.)
func Curve25519SecretKeyFromPrivateKeyPKCS8Base64(b64 string) ([]byte, error) {
	priv, err := signingKeyFromPrivateKeyPKCS8Base64(b64)
	if err != nil {
		return nil, err
	}
	return curve25519SecretKeyFromSigningKey(priv), nil
}

// encodeSSHEd25519PublicKey encodes an Ed25519 public key in the OpenSSH
// "ssh-ed25519 <base64-blob>" format. Mirrors the Rust
// encode_ssh_ed25519_public_key.
func encodeSSHEd25519PublicKey(pub ed25519.PublicKey) string {
	blob := make([]byte, 0, 4+11+4+32)
	blob = appendSSHString(blob, []byte("ssh-ed25519"))
	blob = appendSSHString(blob, pub)
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob)
}

// appendSSHString appends an SSH wire string (4-byte big-endian length prefix
// followed by the bytes) to buf. Mirrors the Rust append_ssh_string.
func appendSSHString(buf, value []byte) []byte {
	var lenPrefix [4]byte
	binary.BigEndian.PutUint32(lenPrefix[:], uint32(len(value)))
	buf = append(buf, lenPrefix[:]...)
	return append(buf, value...)
}
