package networkproxy

import (
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// newTestCA loads-or-creates a CA inside a temp dir, returning the CA and paths.
func newTestCA(t *testing.T) (*ManagedMitmCA, string, string) {
	t.Helper()
	dir := t.TempDir()
	certPath := filepath.Join(dir, managedMitmCADir, managedMitmCACert)
	keyPath := filepath.Join(dir, managedMitmCADir, managedMitmCAKey)
	ca, err := loadOrCreateMitmCAAt(certPath, keyPath)
	if err != nil {
		t.Fatalf("loadOrCreateMitmCAAt: %v", err)
	}
	return ca, certPath, keyPath
}

func TestManagedMitmCACreatesAndPersists(t *testing.T) {
	ca, certPath, keyPath := newTestCA(t)
	if !fileExists(certPath) {
		t.Fatalf("CA cert was not persisted at %s", certPath)
	}
	if !fileExists(keyPath) {
		t.Fatalf("CA key was not persisted at %s", keyPath)
	}
	if len(ca.CertificatePEM()) == 0 {
		t.Fatal("CertificatePEM returned empty bytes")
	}

	// Reloading must reuse the same key (no overwrite). Mirrors the create-new
	// semantics: a second load_or_create reads the existing material.
	ca2, err := loadOrCreateMitmCAAt(certPath, keyPath)
	if err != nil {
		t.Fatalf("reload CA: %v", err)
	}
	if string(ca2.CertificatePEM()) != string(ca.CertificatePEM()) {
		t.Error("reloaded CA cert differs from original; key was overwritten")
	}
}

func TestManagedMitmCAKeyPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission semantics")
	}
	_, _, keyPath := newTestCA(t)
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("CA key mode = %o, want group/world inaccessible", mode)
	}
}

func TestValidateExistingCAKeyFileRejectsGroupWorld(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission semantics")
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ca.key")
	if err := os.WriteFile(keyPath, []byte("key"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := validateExistingCAKeyFile(keyPath)
	if err == nil || !contains(err.Error(), "group/world accessible") {
		t.Errorf("err = %v, want group/world accessible", err)
	}
}

func TestValidateExistingCAKeyFileRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix symlink semantics")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "real.key")
	link := filepath.Join(dir, "ca.key")
	if err := os.WriteFile(target, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	err := validateExistingCAKeyFile(link)
	if err == nil || !contains(err.Error(), "symlink") {
		t.Errorf("err = %v, want symlink rejection", err)
	}
}

func TestLoadOrCreateCARejectsHalfPresent(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, managedMitmCACert)
	keyPath := filepath.Join(dir, managedMitmCAKey)
	if err := os.WriteFile(certPath, []byte("cert"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := loadOrCreateCAPEM(certPath, keyPath)
	if err == nil || !contains(err.Error(), "both managed MITM CA files must exist") {
		t.Errorf("err = %v, want both-or-neither error", err)
	}
}

func TestIssueHostCertificateDNSAndIP(t *testing.T) {
	ca, _, _ := newTestCA(t)

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca.CertificatePEM()) {
		t.Fatal("failed to add CA to pool")
	}

	cases := []struct {
		name string
		host string
	}{
		{"dns", "api.github.com"},
		{"ipv4", "10.0.0.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tlsConfig, err := ca.TLSConfigForHost(tc.host)
			if err != nil {
				t.Fatalf("TLSConfigForHost: %v", err)
			}
			leaf := tlsConfig.Certificates[0].Leaf
			if leaf == nil {
				t.Fatal("leaf certificate is nil")
			}
			// The minted leaf must verify against the CA for the given host.
			opts := x509.VerifyOptions{Roots: roots, DNSName: ""}
			if net.ParseIP(tc.host) == nil {
				opts.DNSName = tc.host
			}
			if _, err := leaf.Verify(opts); err != nil {
				t.Errorf("leaf failed to verify against CA: %v", err)
			}
			// ALPN advertises h2 + http/1.1, mirroring certs.rs.
			if len(tlsConfig.NextProtos) != 2 || tlsConfig.NextProtos[0] != "h2" || tlsConfig.NextProtos[1] != "http/1.1" {
				t.Errorf("NextProtos = %v, want [h2 http/1.1]", tlsConfig.NextProtos)
			}
		})
	}
}
