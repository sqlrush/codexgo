package plugins

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestPackUnpackRoundTrip(t *testing.T) {
	src := filepath.Join(t.TempDir(), "plugin")
	if err := os.MkdirAll(filepath.Join(src, ".codex-plugin"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, ".codex-plugin", "plugin.json"), []byte(`{"name":"demo"}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(src, "skills", "a"), 0o755); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "skills", "a", "SKILL.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	data, err := PackPluginBundleTarGz(src, 10*1024*1024)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "out")
	if err := UnpackPluginBundleTarGz(data, dst, 10*1024*1024); err != nil {
		t.Fatalf("unpack: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dst, "skills", "a", "SKILL.md"))
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("content got %q", got)
	}
	if _, err := os.Stat(filepath.Join(dst, ".codex-plugin", "plugin.json")); err != nil {
		t.Fatalf("manifest not extracted: %v", err)
	}
}

func TestPackRequiresManifest(t *testing.T) {
	src := filepath.Join(t.TempDir(), "plugin")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := PackPluginBundleTarGz(src, 1024); err == nil {
		t.Fatal("expected error for missing manifest")
	}
}

func TestPackEnforcesMaxBytes(t *testing.T) {
	src := filepath.Join(t.TempDir(), "plugin")
	if err := os.MkdirAll(filepath.Join(src, ".codex-plugin"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, ".codex-plugin", "plugin.json"), []byte(`{"name":"demo"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	big := bytes.Repeat([]byte("x"), 4096)
	if err := os.WriteFile(filepath.Join(src, "big.bin"), big, 0o644); err != nil {
		t.Fatalf("write big: %v", err)
	}
	if _, err := PackPluginBundleTarGz(src, 10); err == nil {
		t.Fatal("expected size limit error")
	}
}

func TestUnpackRejectsTraversal(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("evil")
	_ = tw.WriteHeader(&tar.Header{Name: "../escape.txt", Typeflag: tar.TypeReg, Size: int64(len(body)), Mode: 0o644})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gz.Close()

	dst := filepath.Join(t.TempDir(), "out")
	err := UnpackPluginBundleTarGz(buf.Bytes(), dst, 1024)
	if err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestUnpackRejectsSymlink(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0o777})
	_ = tw.Close()
	_ = gz.Close()

	dst := filepath.Join(t.TempDir(), "out")
	if err := UnpackPluginBundleTarGz(buf.Bytes(), dst, 1024); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestUnpackEnforcesTotalSize(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := bytes.Repeat([]byte("a"), 100)
	_ = tw.WriteHeader(&tar.Header{Name: "f.txt", Typeflag: tar.TypeReg, Size: int64(len(body)), Mode: 0o644})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gz.Close()

	dst := filepath.Join(t.TempDir(), "out")
	if err := UnpackPluginBundleTarGz(buf.Bytes(), dst, 10); err == nil {
		t.Fatal("expected total size rejection")
	}
}
