package networkproxy

// This file is a faithful Go port of codex-rs/network-proxy/src/certs.rs's
// ManagedMitmCa. It manages a per-install MITM certificate authority and mints
// short-lived per-host leaf certificates so the proxy can terminate TLS after a
// CONNECT and enforce policy on the decrypted inner request.
//
// Rust mirrors:
//   - ManagedMitmCa::load_or_create  -> LoadOrCreateMitmCA
//   - ManagedMitmCa::tls_acceptor_data_for_host -> (*ManagedMitmCA).TLSConfigForHost
//   - issue_host_certificate_pem -> (*ManagedMitmCA).issueHostCertificate
//   - generate_ca -> generateCA
//   - load_or_create_ca / managed_ca_paths -> loadOrCreateCAPEM / managedCAPaths
//   - validate_existing_ca_key_file -> validateExistingCAKeyFile

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/sqlrush/codexgo/internal/utils/homedir"
)

const (
	// managedMitmCADir is the subdirectory of CODEXGO_HOME holding the CA material.
	// Mirrors certs.rs MANAGED_MITM_CA_DIR.
	managedMitmCADir = "proxy"
	// managedMitmCACert is the CA certificate filename. Mirrors MANAGED_MITM_CA_CERT.
	managedMitmCACert = "ca.pem"
	// managedMitmCAKey is the CA private-key filename. Mirrors MANAGED_MITM_CA_KEY.
	managedMitmCAKey = "ca.key"

	// caCommonName is the CA certificate subject CN. Mirrors generate_ca's
	// DnType::CommonName value verbatim.
	caCommonName = "network_proxy MITM CA"

	// leafCertValidity bounds minted leaf certificates. rcgen defaults are used in
	// Rust; we pick a generous window so terminated handshakes succeed regardless
	// of host clock skew. The leaf is ephemeral (regenerated per CONNECT) so the
	// long validity has no security cost.
	leafCertValidity = 365 * 24 * time.Hour
	caCertValidity   = 10 * 365 * 24 * time.Hour
)

// ManagedMitmCA holds the parsed CA certificate and key used to sign per-host
// leaf certificates. It is safe for concurrent use; leaf minting caches the
// resulting tls.Certificate per host. Mirrors certs.rs ManagedMitmCa.
type ManagedMitmCA struct {
	caCert *x509.Certificate
	caKey  *ecdsa.PrivateKey
	caPEM  []byte

	mu    sync.Mutex
	cache map[string]*tls.Certificate
}

// LoadOrCreateMitmCA loads the managed CA from CODEXGO_HOME/proxy, creating it on
// first use. Mirrors ManagedMitmCa::load_or_create.
func LoadOrCreateMitmCA() (*ManagedMitmCA, error) {
	certPath, keyPath, err := managedCAPaths()
	if err != nil {
		return nil, err
	}
	return loadOrCreateMitmCAAt(certPath, keyPath)
}

// loadOrCreateMitmCAAt is the path-injectable core used by LoadOrCreateMitmCA and
// tests. It mirrors load_or_create_ca followed by ManagedMitmCa::load_or_create.
func loadOrCreateMitmCAAt(certPath, keyPath string) (*ManagedMitmCA, error) {
	certPEM, keyPEM, err := loadOrCreateCAPEM(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	caCert, caKey, err := parseCAPEM(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &ManagedMitmCA{
		caCert: caCert,
		caKey:  caKey,
		caPEM:  certPEM,
		cache:  make(map[string]*tls.Certificate),
	}, nil
}

// CertificatePEM returns the CA certificate in PEM form, suitable for adding to a
// client trust store (e.g. tests dialing through the proxy).
func (c *ManagedMitmCA) CertificatePEM() []byte {
	out := make([]byte, len(c.caPEM))
	copy(out, c.caPEM)
	return out
}

// TLSConfigForHost returns a *tls.Config that presents a freshly minted leaf
// certificate for host, signed by the managed CA. Mirrors
// ManagedMitmCa::tls_acceptor_data_for_host (which builds a rustls ServerConfig
// advertising h2 + http/1.1; we advertise the same ALPN protocols).
func (c *ManagedMitmCA) TLSConfigForHost(host string) (*tls.Config, error) {
	cert, err := c.certificateForHost(host)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{*cert},
		NextProtos:   []string{"h2", "http/1.1"},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// certificateForHost returns a cached or freshly minted leaf certificate for host.
func (c *ManagedMitmCA) certificateForHost(host string) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cert, ok := c.cache[host]; ok {
		return cert, nil
	}
	cert, err := c.issueHostCertificate(host)
	if err != nil {
		return nil, err
	}
	c.cache[host] = cert
	return cert, nil
}

// issueHostCertificate mints and signs a leaf certificate for host. Mirrors
// issue_host_certificate_pem: an IP literal goes into the IPAddress SAN,
// everything else into a DNS SAN, with ServerAuth EKU and digital-signature /
// key-encipherment key usage.
func (c *ManagedMitmCA) issueHostCertificate(host string) (*tls.Certificate, error) {
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate host key pair: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: host},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(leafCertValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{host}
	}

	der, err := x509.CreateCertificate(rand.Reader, template, c.caCert, &leafKey.PublicKey, c.caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign host cert: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("failed to parse minted leaf cert: %w", err)
	}
	return &tls.Certificate{
		Certificate: [][]byte{der, c.caCert.Raw},
		PrivateKey:  leafKey,
		Leaf:        leaf,
	}, nil
}

// managedCAPaths resolves the CA cert/key paths under CODEXGO_HOME/proxy. Mirrors
// managed_ca_paths.
func managedCAPaths() (certPath, keyPath string, err error) {
	codexHome, err := homedir.FindCodexHome()
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve CODEXGO_HOME for managed MITM CA: %w", err)
	}
	proxyDir := filepath.Join(codexHome, managedMitmCADir)
	return filepath.Join(proxyDir, managedMitmCACert), filepath.Join(proxyDir, managedMitmCAKey), nil
}

// loadOrCreateCAPEM loads the CA cert/key PEM bytes, generating them atomically on
// first use. Mirrors load_or_create_ca, including the both-or-neither invariant,
// the key-permission validation, and create-new (never overwrite) semantics.
func loadOrCreateCAPEM(certPath, keyPath string) (certPEM, keyPEM []byte, err error) {
	certExists := fileExists(certPath)
	keyExists := fileExists(keyPath)
	if certExists || keyExists {
		if !certExists || !keyExists {
			return nil, nil, fmt.Errorf("both managed MITM CA files must exist (cert=%s, key=%s)", certPath, keyPath)
		}
		if err := validateExistingCAKeyFile(keyPath); err != nil {
			return nil, nil, err
		}
		certPEM, err = os.ReadFile(certPath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read CA cert %s: %w", certPath, err)
		}
		keyPEM, err = os.ReadFile(keyPath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read CA key %s: %w", keyPath, err)
		}
		return certPEM, keyPEM, nil
	}

	if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
		return nil, nil, fmt.Errorf("failed to create %s: %w", filepath.Dir(certPath), err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o755); err != nil {
		return nil, nil, fmt.Errorf("failed to create %s: %w", filepath.Dir(keyPath), err)
	}

	certPEM, keyPEM, err = generateCA()
	if err != nil {
		return nil, nil, err
	}
	// The CA key is a high-value secret: 0o600. Create-new so we never clobber an
	// existing key (which would invalidate previously-trusted chains).
	if err := writeAtomicCreateNew(keyPath, keyPEM, 0o600); err != nil {
		return nil, nil, fmt.Errorf("failed to persist CA key %s: %w", keyPath, err)
	}
	if err := writeAtomicCreateNew(certPath, certPEM, 0o644); err != nil {
		// Avoid leaving a partially-created CA around (cert missing).
		_ = os.Remove(keyPath)
		return nil, nil, fmt.Errorf("failed to persist CA cert %s: %w", certPath, err)
	}
	return certPEM, keyPEM, nil
}

// generateCA creates a fresh self-signed CA certificate and key. Mirrors
// generate_ca: ECDSA P-256, key-cert-sign + digital-signature + key-encipherment
// key usage, and the fixed CN.
func generateCA() (certPEM, keyPEM []byte, err error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate CA key pair: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: caCommonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(caCertValidity),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate CA cert: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to serialize CA key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

// parseCAPEM parses the CA cert and key from PEM. Mirrors the parsing performed in
// ManagedMitmCa::load_or_create.
func parseCAPEM(certPEM, keyPEM []byte) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return nil, nil, fmt.Errorf("failed to parse CA cert PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse CA cert: %w", err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("failed to parse CA key PEM")
	}
	key, err := parseECKey(keyBlock)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse CA key: %w", err)
	}
	return cert, key, nil
}

// parseECKey parses an EC private key from either SEC1 ("EC PRIVATE KEY") or
// PKCS#8 ("PRIVATE KEY") PEM blocks.
func parseECKey(block *pem.Block) (*ecdsa.PrivateKey, error) {
	switch block.Type {
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	case "PRIVATE KEY":
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		key, ok := parsed.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("CA key is not an ECDSA key")
		}
		return key, nil
	default:
		return nil, fmt.Errorf("unsupported CA key PEM type %q", block.Type)
	}
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to generate certificate serial: %w", err)
	}
	return serial, nil
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// validateExistingCAKeyFile mirrors validate_existing_ca_key_file: reject symlinks
// and group/world-accessible key files on unix. On non-unix it is a no-op.
func validateExistingCAKeyFile(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("failed to stat CA key %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to use symlink for managed MITM CA key %s", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("managed MITM CA key is not a regular file: %s", path)
	}
	mode := info.Mode().Perm()
	if mode&0o077 != 0 {
		return fmt.Errorf("managed MITM CA key %s must not be group/world accessible (mode=%o; expected <= 600)", path, mode)
	}
	return nil
}

// writeAtomicCreateNew writes contents to a temp file then links it into place
// with create-new semantics, refusing to overwrite an existing file. Mirrors
// write_atomic_create_new (the hard-link-then-remove dance to get no-overwrite
// rename behavior on unix).
func writeAtomicCreateNew(path string, contents []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp := filepath.Join(dir, fmt.Sprintf(".%s.tmp.%d.%d", filepath.Base(path), os.Getpid(), time.Now().UnixNano()))

	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", tmp, err)
	}
	if _, err := f.Write(contents); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to write %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to fsync %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to close %s: %w", tmp, err)
	}

	if err := os.Link(tmp, path); err != nil {
		_ = os.Remove(tmp)
		if os.IsExist(err) {
			return fmt.Errorf("refusing to overwrite existing file %s", path)
		}
		// Fallback for filesystems without hard links: refuse to overwrite, then
		// rename. Subject to a TOCTOU race, acceptable for a per-user dir.
		if fileExists(path) {
			return fmt.Errorf("refusing to overwrite existing file %s", path)
		}
		if rerr := os.Rename(tmp, path); rerr != nil {
			return fmt.Errorf("failed to rename %s -> %s: %w", tmp, path, rerr)
		}
		return nil
	}
	if err := os.Remove(tmp); err != nil {
		return fmt.Errorf("failed to remove %s: %w", tmp, err)
	}
	return nil
}
