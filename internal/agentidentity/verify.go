package agentidentity

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/golang-jwt/jwt/v5"
)

// JWKSet is a minimal JSON Web Key Set holding the RSA signing keys used to
// verify agent-identity JWTs. Only the fields needed for RS256 verification are
// modeled, matching the keys consumed by the Rust jsonwebtoken JwkSet path.
type JWKSet struct {
	Keys []JWK `json:"keys"`
}

// JWK is a single JSON Web Key. Only RSA signing keys ("kty":"RSA") are
// supported; other key types are ignored during lookup.
type JWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// ParseJWKSet parses a JWKS document from its JSON encoding.
func ParseJWKSet(data []byte) (*JWKSet, error) {
	var set JWKSet
	if err := json.Unmarshal(data, &set); err != nil {
		return nil, fmt.Errorf("failed to decode agent identity JWKS: %w", err)
	}
	return &set, nil
}

// find returns the JWK with the given key id, or false when none matches.
func (s *JWKSet) find(kid string) (JWK, bool) {
	for _, key := range s.Keys {
		if key.Kid == kid {
			return key, true
		}
	}
	return JWK{}, false
}

// rsaPublicKey builds an rsa.PublicKey from the JWK's base64url modulus and
// exponent. It mirrors jsonwebtoken's DecodingKey::from_jwk for RSA keys.
func (k JWK) rsaPublicKey() (*rsa.PublicKey, error) {
	if k.Kty != "RSA" {
		return nil, fmt.Errorf("agent identity JWK kty %q is not RSA", k.Kty)
	}
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWK modulus: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWK exponent: %w", err)
	}
	e := new(big.Int).SetBytes(eBytes)
	if !e.IsInt64() || e.Int64() > int64(^uint32(0)) {
		return nil, fmt.Errorf("JWK exponent is out of range")
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(e.Int64()),
	}, nil
}

// jwt.Claims interface implementation for JwtClaims. These let the golang-jwt
// validator enforce the registered claims (exp/iat) and the issuer/audience
// requirements set on the verified decode path.

// GetExpirationTime returns the `exp` claim as a NumericDate.
func (c JwtClaims) GetExpirationTime() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(unixTime(c.Exp)), nil
}

// GetIssuedAt returns the `iat` claim as a NumericDate.
func (c JwtClaims) GetIssuedAt() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(unixTime(c.Iat)), nil
}

// GetNotBefore returns nil; agent-identity JWTs do not carry an `nbf` claim.
func (c JwtClaims) GetNotBefore() (*jwt.NumericDate, error) {
	return nil, nil
}

// GetIssuer returns the `iss` claim.
func (c JwtClaims) GetIssuer() (string, error) { return c.Iss, nil }

// GetSubject returns an empty subject; agent-identity JWTs do not carry `sub`.
func (c JwtClaims) GetSubject() (string, error) { return "", nil }

// GetAudience returns the `aud` claim as a single-element ClaimStrings.
func (c JwtClaims) GetAudience() (jwt.ClaimStrings, error) {
	return jwt.ClaimStrings{c.Aud}, nil
}

// VerifyJwtClaims verifies an agent-identity JWT against the supplied JWKS,
// validating the RS256 signature, the `kid` header against a trusted key, and
// the required issuer/audience claims. It mirrors the Rust
// decode_agent_identity_jwt(jwt, Some(jwks)) path.
func VerifyJwtClaims(token string, jwks *JWKSet) (JwtClaims, error) {
	if jwks == nil {
		return DecodeJwtClaims(token)
	}

	var claims JwtClaims
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(JWTIssuer),
		jwt.WithAudience(JWTAudience),
		jwt.WithExpirationRequired(),
	)
	keyFunc := func(t *jwt.Token) (any, error) {
		kidRaw, ok := t.Header["kid"]
		if !ok {
			return nil, fmt.Errorf("agent identity JWT header does not include a kid")
		}
		kid, ok := kidRaw.(string)
		if !ok {
			return nil, fmt.Errorf("agent identity JWT kid is not a string")
		}
		jwk, ok := jwks.find(kid)
		if !ok {
			return nil, fmt.Errorf("agent identity JWT kid %s is not trusted", kid)
		}
		return jwk.rsaPublicKey()
	}
	if _, err := parser.ParseWithClaims(token, &claims, keyFunc); err != nil {
		return JwtClaims{}, fmt.Errorf("failed to verify agent identity JWT: %w", err)
	}
	return claims, nil
}
