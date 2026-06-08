package dbaa

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleConfig = `
connections:
  - name: pg
    db_type: postgres
    host: 127.0.0.1
    port: 5432
    database: postgres
    user: postgres
    credential:
      value: pgsecret
  - name: og
    db_type: opengauss
    host: 10.0.0.1
    port: 5433
    database: postgres
    user: gaussdb
    credential:
      value: ogsecret
  - name: gauss_local
    db_type: gaussdb
    host: 127.0.0.1
    port: 8000
    database: postgres
    user: dbmon
    credential:
      value: gsecret
`

func writeConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(sampleConfig), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DBAA_CONFIG", path)
	return path
}

func TestGaussConnectionsFiltersTypes(t *testing.T) {
	writeConfig(t)
	conns, err := GaussConnections()
	if err != nil {
		t.Fatalf("GaussConnections: %v", err)
	}
	// Only og (opengauss) and gauss_local (gaussdb); pg excluded.
	if len(conns) != 2 {
		t.Fatalf("expected 2 gauss/og connections, got %d", len(conns))
	}
}

func TestResolveDefaultPrefersGaussdb(t *testing.T) {
	writeConfig(t)
	target, label, err := Resolve("")
	if err != nil {
		t.Fatalf("Resolve(default): %v", err)
	}
	// Default prefers db_type=gaussdb -> gauss_local, not the earlier og entry.
	if target.Host != "127.0.0.1" || target.Port != 8000 || target.User != "dbmon" || target.Password != "gsecret" {
		t.Errorf("default target wrong: %+v", target)
	}
	if label == "" {
		t.Error("expected a non-empty label")
	}
}

func TestResolveByName(t *testing.T) {
	writeConfig(t)
	target, _, err := Resolve("og")
	if err != nil {
		t.Fatalf("Resolve(og): %v", err)
	}
	if target.Host != "10.0.0.1" || target.Port != 5433 || target.User != "gaussdb" {
		t.Errorf("og target wrong: %+v", target)
	}
}

func TestResolveUnknownName(t *testing.T) {
	writeConfig(t)
	if _, _, err := Resolve("nope"); err == nil {
		t.Error("expected error for unknown connection name")
	}
}

func TestLoadMissingFile(t *testing.T) {
	t.Setenv("DBAA_CONFIG", filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if _, err := Load(); err == nil {
		t.Error("expected error for missing config file")
	}
}
