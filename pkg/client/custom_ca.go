package client

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
)

// Environment variables that select a custom CA bundle, mirroring the Rust
// `custom_ca` module. CODEXGO_CA_CERTIFICATE takes precedence over SSL_CERT_FILE.
const (
	// CodexCACertEnv is the Codex-specific custom CA override.
	CodexCACertEnv = "CODEXGO_CA_CERTIFICATE"
	// SSLCertFileEnv is the fallback CA override understood by other tooling.
	SSLCertFileEnv = "SSL_CERT_FILE"
)

const caCertHint = "If you set CODEXGO_CA_CERTIFICATE or SSL_CERT_FILE, ensure it points to a PEM file containing one or more CERTIFICATE blocks, or unset it to use system roots."

// BuildCustomCATransportError describes why a transport using shared custom CA
// support could not be constructed. It mirrors the Rust
// `BuildCustomCaTransportError`.
type BuildCustomCATransportError struct {
	SourceEnv string
	Path      string
	Detail    string
	// Err is the wrapped underlying error, if any.
	Err error
}

// Error implements the error interface.
func (e *BuildCustomCATransportError) Error() string {
	return fmt.Sprintf("%s (CA file %q selected by %s). %s", e.Detail, e.Path, e.SourceEnv, caCertHint)
}

// Unwrap returns the wrapped error.
func (e *BuildCustomCATransportError) Unwrap() error { return e.Err }

// configuredCABundle is the result of environment-precedence selection.
type configuredCABundle struct {
	sourceEnv string
	path      string
}

// EnvSource abstracts environment access so tests can cover precedence rules
// without mutating process-wide variables. It mirrors the Rust `EnvSource`
// trait.
type EnvSource interface {
	// Var returns the raw value of key, and whether it was set.
	Var(key string) (string, bool)
}

// ProcessEnv reads CA configuration from the real process environment.
type ProcessEnv struct{}

// Var implements EnvSource using os.LookupEnv.
func (ProcessEnv) Var(key string) (string, bool) { return os.LookupEnv(key) }

// nonEmptyPath returns the value of key interpreted as a path, treating empty
// strings as unset. It mirrors the Rust `EnvSource::non_empty_path`.
func nonEmptyPath(env EnvSource, key string) (string, bool) {
	value, ok := env.Var(key)
	if !ok || value == "" {
		return "", false
	}
	return value, true
}

// configuredBundle returns the selected CA bundle and which env var selected it.
// CODEXGO_CA_CERTIFICATE wins over SSL_CERT_FILE. It mirrors the Rust
// `EnvSource::configured_ca_bundle`.
func configuredBundle(env EnvSource) (configuredCABundle, bool) {
	if path, ok := nonEmptyPath(env, CodexCACertEnv); ok {
		return configuredCABundle{sourceEnv: CodexCACertEnv, path: path}, true
	}
	if path, ok := nonEmptyPath(env, SSLCertFileEnv); ok {
		return configuredCABundle{sourceEnv: SSLCertFileEnv, path: path}, true
	}
	return configuredCABundle{}, false
}

// MaybeBuildTLSConfigWithCustomCA returns a *tls.Config seeded with the system
// roots plus any configured custom CA bundle, or (nil, nil) when no custom CA env
// var is set. It is the Go analogue of the Rust
// `maybe_build_rustls_client_config_with_custom_ca`: callers that get nil should
// keep their default TLS behavior.
func MaybeBuildTLSConfigWithCustomCA() (*tls.Config, error) {
	return maybeBuildTLSConfigWithEnv(ProcessEnv{})
}

func maybeBuildTLSConfigWithEnv(env EnvSource) (*tls.Config, error) {
	bundle, ok := configuredBundle(env)
	if !ok {
		return nil, nil
	}

	// Start from the platform roots so callers keep the same baseline trust
	// behavior, then layer in the custom CA bundle on top.
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}

	certs, err := loadCertificates(bundle)
	if err != nil {
		return nil, err
	}
	for _, cert := range certs {
		pool.AddCert(cert)
	}

	return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, nil
}

// BuildHTTPClientTLSConfig returns the custom-CA tls.Config to apply to an
// http.Client transport, or nil when no override is configured. It is the Go
// analogue of `build_reqwest_client_with_custom_ca`: when nil is returned the
// caller should use system roots.
func BuildHTTPClientTLSConfig() (*tls.Config, error) {
	return MaybeBuildTLSConfigWithCustomCA()
}

// loadCertificates loads every certificate block from the bundle's PEM file. It
// mirrors the Rust `ConfiguredCaBundle::parse_certificates`, including
// normalization of OpenSSL `TRUSTED CERTIFICATE` labels and ignoring CRL blocks.
func loadCertificates(bundle configuredCABundle) ([]*x509.Certificate, error) {
	data, err := os.ReadFile(bundle.path)
	if err != nil {
		return nil, &BuildCustomCATransportError{
			SourceEnv: bundle.sourceEnv,
			Path:      bundle.path,
			Detail:    fmt.Sprintf("failed to read CA certificate file: %v", err),
			Err:       err,
		}
	}

	normalized := normalizePEM(data)

	var certs []*x509.Certificate
	rest := normalized
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		switch {
		case block.Type == "CERTIFICATE" || block.Type == "TRUSTED CERTIFICATE":
			der := firstDERItem(block.Bytes)
			cert, perr := x509.ParseCertificate(der)
			if perr != nil {
				return nil, &BuildCustomCATransportError{
					SourceEnv: bundle.sourceEnv,
					Path:      bundle.path,
					Detail:    fmt.Sprintf("failed to parse certificate: %v", perr),
					Err:       perr,
				}
			}
			certs = append(certs, cert)
		default:
			// Ignore non-certificate blocks (e.g. X509 CRL), matching Rust.
		}
	}

	if len(certs) == 0 {
		return nil, &BuildCustomCATransportError{
			SourceEnv: bundle.sourceEnv,
			Path:      bundle.path,
			Detail:    "no certificates found in PEM file",
		}
	}
	return certs, nil
}

// normalizePEM rewrites OpenSSL `TRUSTED CERTIFICATE` labels to standard
// `CERTIFICATE` labels so the PEM decoder treats them as certificate input. It
// mirrors the Rust `NormalizedPem::from_pem_data`.
func normalizePEM(data []byte) []byte {
	text := string(data)
	if !strings.Contains(text, "TRUSTED CERTIFICATE") {
		return data
	}
	text = strings.ReplaceAll(text, "BEGIN TRUSTED CERTIFICATE", "BEGIN CERTIFICATE")
	text = strings.ReplaceAll(text, "END TRUSTED CERTIFICATE", "END CERTIFICATE")
	return []byte(text)
}

// firstDERItem returns the first top-level DER object in der, discarding any
// trailing OpenSSL X509_AUX trust metadata that TRUSTED CERTIFICATE blocks may
// append. It mirrors the Rust `first_der_item` / `der_item_length`.
func firstDERItem(der []byte) []byte {
	length := derItemLength(der)
	if length < 0 || length > len(der) {
		return der
	}
	return der[:length]
}

// derItemLength returns the byte length of the first DER item in der, or -1 if
// it cannot be determined. It mirrors the Rust `der_item_length` and supports
// both short- and long-form length encodings, rejecting indefinite lengths.
func derItemLength(der []byte) int {
	if len(der) < 2 {
		return -1
	}
	lengthOctet := der[1]
	if lengthOctet&0x80 == 0 {
		length := 2 + int(lengthOctet)
		if length > len(der) {
			return -1
		}
		return length
	}

	lengthOctets := int(lengthOctet & 0x7f)
	if lengthOctets == 0 {
		return -1
	}
	const lengthStart = 2
	lengthEnd := lengthStart + lengthOctets
	if lengthEnd > len(der) {
		return -1
	}
	contentLength := 0
	for _, b := range der[lengthStart:lengthEnd] {
		// Guard against overflow on absurd declared lengths.
		if contentLength > (1 << 58) {
			return -1
		}
		contentLength = (contentLength << 8) | int(b)
	}
	total := lengthEnd + contentLength
	if total > len(der) {
		return -1
	}
	return total
}
