package client

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeEnv map[string]string

func (f fakeEnv) Var(key string) (string, bool) {
	v, ok := f[key]
	return v, ok
}

func TestConfiguredBundlePrecedence(t *testing.T) {
	env := fakeEnv{CodexCACertEnv: "/codex.pem", SSLCertFileEnv: "/ssl.pem"}
	bundle, ok := configuredBundle(env)
	if !ok || bundle.sourceEnv != CodexCACertEnv || bundle.path != "/codex.pem" {
		t.Fatalf("expected codex env to win, got %+v ok=%v", bundle, ok)
	}
}

func TestConfiguredBundleFallback(t *testing.T) {
	env := fakeEnv{SSLCertFileEnv: "/ssl.pem"}
	bundle, ok := configuredBundle(env)
	if !ok || bundle.sourceEnv != SSLCertFileEnv {
		t.Fatalf("expected ssl fallback, got %+v ok=%v", bundle, ok)
	}
}

func TestConfiguredBundleEmptyTreatedAsUnset(t *testing.T) {
	env := fakeEnv{CodexCACertEnv: "", SSLCertFileEnv: ""}
	if _, ok := configuredBundle(env); ok {
		t.Fatalf("empty values should be treated as unset")
	}
}

func TestMaybeBuildTLSConfigNoEnvReturnsNil(t *testing.T) {
	cfg, err := maybeBuildTLSConfigWithEnv(fakeEnv{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config when no env set")
	}
}

func writeTestCertPEM(t *testing.T, label string) (string, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "codexgo-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: label, Bytes: der})
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatalf("write pem: %v", err)
	}
	return path, cert
}

func TestLoadCertificatesStandardLabel(t *testing.T) {
	path, _ := writeTestCertPEM(t, "CERTIFICATE")
	certs, err := loadCertificates(configuredCABundle{sourceEnv: CodexCACertEnv, path: path})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(certs) != 1 {
		t.Fatalf("expected 1 cert, got %d", len(certs))
	}
}

func TestLoadCertificatesNormalizesTrustedLabel(t *testing.T) {
	path, _ := writeTestCertPEM(t, "TRUSTED CERTIFICATE")
	certs, err := loadCertificates(configuredCABundle{sourceEnv: CodexCACertEnv, path: path})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(certs) != 1 {
		t.Fatalf("expected 1 cert, got %d", len(certs))
	}
}

func TestLoadCertificatesEmptyFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.pem")
	if err := os.WriteFile(path, []byte("not a pem"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := loadCertificates(configuredCABundle{sourceEnv: CodexCACertEnv, path: path})
	if err == nil {
		t.Fatalf("expected error for empty PEM")
	}
	var caErr *BuildCustomCATransportError
	if !asCAErr(err, &caErr) {
		t.Fatalf("expected BuildCustomCATransportError, got %T", err)
	}
	if !strings.Contains(caErr.Error(), "no certificates found") {
		t.Fatalf("unexpected error: %v", caErr)
	}
}

func TestLoadCertificatesReadError(t *testing.T) {
	_, err := loadCertificates(configuredCABundle{sourceEnv: CodexCACertEnv, path: "/nonexistent/path/ca.pem"})
	if err == nil {
		t.Fatalf("expected read error")
	}
}

func TestMaybeBuildTLSConfigWithCustomCA(t *testing.T) {
	path, _ := writeTestCertPEM(t, "CERTIFICATE")
	cfg, err := maybeBuildTLSConfigWithEnv(fakeEnv{CodexCACertEnv: path})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if cfg == nil || cfg.RootCAs == nil {
		t.Fatalf("expected tls config with root CAs")
	}
}

func TestDerItemLengthShortForm(t *testing.T) {
	// SEQUENCE tag 0x30, length 0x02, two content bytes.
	der := []byte{0x30, 0x02, 0xAA, 0xBB, 0xCC}
	if got := derItemLength(der); got != 4 {
		t.Fatalf("derItemLength short form = %d, want 4", got)
	}
}

func asCAErr(err error, target **BuildCustomCATransportError) bool {
	ce, ok := err.(*BuildCustomCATransportError)
	if ok {
		*target = ce
	}
	return ok
}
