package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/config"
)

// writeMarketplaceManifest writes a minimal valid marketplace.json under root.
func writeMarketplaceManifest(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, ".agents", "plugins")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir manifest dir: %v", err)
	}
	contents := fmt.Sprintf(`{"name":%q,"interface":{"displayName":%q},"plugins":[]}`, name, name)
	if err := os.WriteFile(filepath.Join(dir, "marketplace.json"), []byte(contents), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func gitMarketplaceConfig(source, lastRevision string) config.MarketplaceConfig {
	src := source
	st := config.MarketplaceSourceGit
	mc := config.MarketplaceConfig{Source: &src, SourceType: &st}
	if lastRevision != "" {
		lr := lastRevision
		mc.LastRevision = &lr
	}
	return mc
}

func TestUpgradeClonesWhenRevisionChanged(t *testing.T) {
	home := t.TempDir()
	source := "https://example.com/mp.git"
	client := &cloningGitClient{t: t, name: "demo-mp", revision: "rev-new"}

	cfg := map[string]config.MarketplaceConfig{
		"demo-mp": gitMarketplaceConfig(source, "rev-old"),
	}
	outcome := UpgradeConfiguredGitMarketplacesWithClient(home, cfg, "", client)
	if !outcome.AllSucceeded() {
		t.Fatalf("expected success, errors: %+v", outcome.Errors)
	}
	if len(outcome.UpgradedRoots) != 1 {
		t.Fatalf("expected 1 upgraded root, got %d", len(outcome.UpgradedRoots))
	}
	if client.cloneCalls != 1 {
		t.Fatalf("expected 1 clone, got %d", client.cloneCalls)
	}
	// The activated root carries the manifest and the install metadata.
	dest := filepath.Join(MarketplaceInstallRoot(home), "demo-mp")
	if _, ok := FindMarketplaceManifestPath(dest); !ok {
		t.Fatalf("activated root missing manifest")
	}
	if _, err := os.Stat(filepath.Join(dest, marketplaceInstallMetadataFile)); err != nil {
		t.Fatalf("activated root missing install metadata: %v", err)
	}
}

func TestUpgradeSkipsWhenUpToDate(t *testing.T) {
	home := t.TempDir()
	source := "https://example.com/mp.git"
	const rev = "rev-current"

	// Pre-stage an already-activated marketplace at the current revision so the
	// update decision skips the re-clone.
	dest := filepath.Join(MarketplaceInstallRoot(home), "demo-mp")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatalf("mkdir dest: %v", err)
	}
	writeMarketplaceManifest(t, dest, "demo-mp")
	writeInstalledMarketplaceMetadata(dest, configuredGitMarketplace{
		name:   "demo-mp",
		source: source,
	}, rev)

	client := &cloningGitClient{t: t, name: "demo-mp", revision: rev}
	cfg := map[string]config.MarketplaceConfig{
		"demo-mp": gitMarketplaceConfig(source, rev),
	}
	outcome := UpgradeConfiguredGitMarketplacesWithClient(home, cfg, "", client)
	if !outcome.AllSucceeded() {
		t.Fatalf("expected success, errors: %+v", outcome.Errors)
	}
	if len(outcome.UpgradedRoots) != 0 {
		t.Fatalf("expected no upgrades when up to date, got %d", len(outcome.UpgradedRoots))
	}
	if client.cloneCalls != 0 {
		t.Fatalf("expected no clone when up to date, got %d", client.cloneCalls)
	}
}

func TestUpgradeFailureIsolation(t *testing.T) {
	home := t.TempDir()
	okSource := "https://example.com/ok.git"
	badSource := "https://example.com/bad.git"

	client := &multiGitClient{
		t: t,
		revisions: map[string]string{
			okSource:  "rev-ok",
			badSource: "rev-bad",
		},
		names: map[string]string{
			okSource:  "ok-mp",
			badSource: "bad-mp",
		},
		remoteErr: map[string]error{
			badSource: fmt.Errorf("network down"),
		},
	}
	cfg := map[string]config.MarketplaceConfig{
		"ok-mp":  gitMarketplaceConfig(okSource, "rev-old"),
		"bad-mp": gitMarketplaceConfig(badSource, "rev-old"),
	}
	outcome := UpgradeConfiguredGitMarketplacesWithClient(home, cfg, "", client)

	// Both selected; one succeeds, one fails — the failure does not abort the
	// other.
	if len(outcome.SelectedMarketplaces) != 2 {
		t.Fatalf("expected 2 selected, got %v", outcome.SelectedMarketplaces)
	}
	if len(outcome.UpgradedRoots) != 1 {
		t.Fatalf("expected 1 upgraded root, got %d", len(outcome.UpgradedRoots))
	}
	if len(outcome.Errors) != 1 || outcome.Errors[0].MarketplaceName != "bad-mp" {
		t.Fatalf("expected one bad-mp error, got %+v", outcome.Errors)
	}
	if outcome.AllSucceeded() {
		t.Fatal("expected AllSucceeded to be false")
	}
}

func TestUpgradeNameFilterAndEmpty(t *testing.T) {
	home := t.TempDir()
	source := "https://example.com/mp.git"
	client := &cloningGitClient{t: t, name: "demo-mp", revision: "rev-new"}
	cfg := map[string]config.MarketplaceConfig{
		"demo-mp": gitMarketplaceConfig(source, "rev-old"),
	}

	// A name that doesn't exist selects nothing.
	outcome := UpgradeConfiguredGitMarketplacesWithClient(home, cfg, "missing", client)
	if len(outcome.SelectedMarketplaces) != 0 {
		t.Fatalf("expected empty selection for missing name, got %v", outcome.SelectedMarketplaces)
	}

	// Local-source marketplaces are ignored (only git sources upgrade).
	localType := config.MarketplaceSourceLocal
	localSrc := "/local/mp"
	cfgLocal := map[string]config.MarketplaceConfig{
		"local-mp": {Source: &localSrc, SourceType: &localType},
	}
	outcome = UpgradeConfiguredGitMarketplacesWithClient(home, cfgLocal, "", client)
	if len(outcome.SelectedMarketplaces) != 0 {
		t.Fatalf("expected local marketplace ignored, got %v", outcome.SelectedMarketplaces)
	}
}

// cloningGitClient writes a single-named marketplace manifest on clone.
type cloningGitClient struct {
	t          *testing.T
	name       string
	revision   string
	cloneCalls int
}

func (c *cloningGitClient) RemoteRevision(string, *string, time.Duration) (string, error) {
	return c.revision, nil
}

func (c *cloningGitClient) CloneSource(_ string, _ *string, _ []string, destination string, _ time.Duration) (string, error) {
	c.cloneCalls++
	writeMarketplaceManifest(c.t, destination, c.name)
	return c.revision, nil
}

// multiGitClient handles multiple sources with scripted revisions/names/errors.
type multiGitClient struct {
	t         *testing.T
	revisions map[string]string
	names     map[string]string
	remoteErr map[string]error
}

func (m *multiGitClient) RemoteRevision(source string, _ *string, _ time.Duration) (string, error) {
	if err, ok := m.remoteErr[source]; ok {
		return "", err
	}
	return m.revisions[source], nil
}

func (m *multiGitClient) CloneSource(source string, _ *string, _ []string, destination string, _ time.Duration) (string, error) {
	writeMarketplaceManifest(m.t, destination, m.names[source])
	return m.revisions[source], nil
}
