package installcontext

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

const testResourceName = "codex-test-helper"

// canonAbs canonicalizes path into an AbsolutePathBuf for test expectations,
// mirroring the Rust tests' use of `canonicalize` + `from_absolute_path`.
func canonAbs(t *testing.T, path string) abspath.AbsolutePathBuf {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("canonicalize %q: %v", path, err)
	}
	abs, err := abspath.FromAbsolutePathChecked(resolved)
	if err != nil {
		t.Fatalf("from_absolute_path %q: %v", resolved, err)
	}
	return abs
}

func exeName() string {
	if isWindows() {
		return "codex.exe"
	}
	return "codex"
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", path, err)
	}
}

func TestDetectsStandaloneInstallFromReleaseLayout(t *testing.T) {
	codexHome := t.TempDir()
	releaseDir := filepath.Join(codexHome, "packages/standalone/releases/1.2.3-x86_64-unknown-linux-musl")
	resourcesDir := filepath.Join(releaseDir, resourcesDirname)
	mkdirAll(t, resourcesDir)
	exePath := filepath.Join(releaseDir, exeName())
	writeFile(t, exePath, "")
	writeFile(t, filepath.Join(resourcesDir, defaultRgCommand()), "")
	writeFile(t, filepath.Join(resourcesDir, testResourceName), "")

	canonRelease := canonAbs(t, releaseDir)
	canonResources := canonAbs(t, resourcesDir)

	ctx := detectFromExeWithCodexHome(false, exePath, false, false, &codexHome)

	if ctx.Method.Kind() != MethodStandalone {
		t.Fatalf("expected standalone, got %v", ctx.Method.Kind())
	}
	rd, resourcesPtr, platform, ok := ctx.Method.Standalone()
	if !ok {
		t.Fatal("expected standalone payload")
	}
	if rd.Path() != canonRelease.Path() {
		t.Fatalf("release dir got %q want %q", rd.Path(), canonRelease.Path())
	}
	if resourcesPtr == nil || resourcesPtr.Path() != canonResources.Path() {
		t.Fatalf("resources dir got %v want %q", resourcesPtr, canonResources.Path())
	}
	if platform != standalonePlatform() {
		t.Fatalf("platform mismatch")
	}
	if ctx.PackageLayout != nil {
		t.Fatalf("expected no package layout")
	}

	got, ok := ctx.BundledResource(testResourceName)
	if !ok || got.Path() != canonResources.Join(testResourceName).Path() {
		t.Fatalf("bundled resource got (%v,%v)", got, ok)
	}
}

func TestStandaloneRgFallsBackWhenResourcesAreMissing(t *testing.T) {
	codexHome := t.TempDir()
	releaseDir := filepath.Join(codexHome, "packages/standalone/releases/1.2.3-x86_64-unknown-linux-musl")
	mkdirAll(t, releaseDir)
	exePath := filepath.Join(releaseDir, exeName())
	writeFile(t, exePath, "")

	ctx := detectFromExeWithCodexHome(false, exePath, false, false, &codexHome)
	if ctx.RgCommand() != defaultRgCommand() {
		t.Fatalf("rg got %q want %q", ctx.RgCommand(), defaultRgCommand())
	}
}

func TestDetectsPackageLayoutIndependentlyFromInstallMethod(t *testing.T) {
	packageDir := t.TempDir()
	binDir := filepath.Join(packageDir, binDirname)
	resourcesDir := filepath.Join(packageDir, resourcesDirname)
	pathDir := filepath.Join(packageDir, pathDirname)
	mkdirAll(t, binDir)
	mkdirAll(t, resourcesDir)
	mkdirAll(t, pathDir)
	writeFile(t, filepath.Join(packageDir, packageMetadataFilename), "{}")
	exePath := filepath.Join(binDir, exeName())
	writeFile(t, exePath, "")
	writeFile(t, filepath.Join(resourcesDir, testResourceName), "")
	writeFile(t, filepath.Join(pathDir, defaultRgCommand()), "")
	if !isWindows() {
		zshPath := filepath.Join(resourcesDir, zshResourcePath())
		mkdirAll(t, filepath.Dir(zshPath))
		writeFile(t, zshPath, "")
	}

	canonResources := canonAbs(t, resourcesDir)
	canonPathDir := canonAbs(t, pathDir)

	ctx := detectFromExeWithCodexHome(false, exePath, false, false, nil)

	if ctx.Method.Kind() != MethodOther {
		t.Fatalf("expected Other, got %v", ctx.Method.Kind())
	}
	if ctx.PackageLayout == nil {
		t.Fatalf("expected package layout")
	}
	if ctx.PackageLayout.PackageDir.Path() != canonAbs(t, packageDir).Path() {
		t.Fatalf("package dir mismatch")
	}
	if ctx.RgCommand() != canonPathDir.Join(defaultRgCommand()).Path() {
		t.Fatalf("rg got %q want %q", ctx.RgCommand(), canonPathDir.Join(defaultRgCommand()).Path())
	}
	got, ok := ctx.BundledResource(testResourceName)
	if !ok || got.Path() != canonResources.Join(testResourceName).Path() {
		t.Fatalf("bundled resource got (%v,%v)", got, ok)
	}
	if isWindows() {
		if _, ok := ctx.BundledZshPath(); ok {
			t.Fatalf("expected no zsh on windows")
		}
	} else {
		zshPath, ok := ctx.BundledZshPath()
		if !ok || zshPath.Path() != canonResources.Join(zshResourcePath()).Path() {
			t.Fatalf("zsh path got (%v,%v)", zshPath, ok)
		}
		binDirGot, ok := ctx.BundledZshBinDir()
		wantBinDir := canonResources.Join(zshDirname).Join(binDirname)
		if !ok || binDirGot.Path() != wantBinDir.Path() {
			t.Fatalf("zsh bin dir got (%v,%v) want %q", binDirGot, ok, wantBinDir.Path())
		}
	}
}

func TestStandalonePackageLayoutKeepsStandaloneInstallMethod(t *testing.T) {
	codexHome := t.TempDir()
	packageDir := filepath.Join(codexHome, "packages/standalone/releases/1.2.3-x86_64-unknown-linux-musl")
	binDir := filepath.Join(packageDir, binDirname)
	resourcesDir := filepath.Join(packageDir, resourcesDirname)
	pathDir := filepath.Join(packageDir, pathDirname)
	mkdirAll(t, binDir)
	mkdirAll(t, resourcesDir)
	mkdirAll(t, pathDir)
	writeFile(t, filepath.Join(packageDir, packageMetadataFilename), "{}")
	exePath := filepath.Join(binDir, exeName())
	writeFile(t, exePath, "")
	writeFile(t, filepath.Join(resourcesDir, testResourceName), "")
	writeFile(t, filepath.Join(pathDir, defaultRgCommand()), "")

	canonPackage := canonAbs(t, packageDir)
	canonResources := canonAbs(t, resourcesDir)
	canonPathDir := canonAbs(t, pathDir)

	ctx := detectFromExeWithCodexHome(false, exePath, false, false, &codexHome)

	rd, resourcesPtr, platform, ok := ctx.Method.Standalone()
	if !ok {
		t.Fatal("expected standalone")
	}
	if rd.Path() != canonPackage.Path() {
		t.Fatalf("release dir got %q want %q", rd.Path(), canonPackage.Path())
	}
	if resourcesPtr == nil || resourcesPtr.Path() != canonResources.Path() {
		t.Fatalf("resources mismatch")
	}
	if platform != standalonePlatform() {
		t.Fatalf("platform mismatch")
	}
	if ctx.PackageLayout == nil || ctx.PackageLayout.PathDir == nil {
		t.Fatalf("expected package layout with path dir")
	}
	if ctx.RgCommand() != canonPathDir.Join(defaultRgCommand()).Path() {
		t.Fatalf("rg mismatch")
	}
}

func TestNpmManagedPackageKeepsPackageLayout(t *testing.T) {
	packageDir := t.TempDir()
	binDir := filepath.Join(packageDir, binDirname)
	pathDir := filepath.Join(packageDir, pathDirname)
	mkdirAll(t, binDir)
	mkdirAll(t, pathDir)
	writeFile(t, filepath.Join(packageDir, packageMetadataFilename), "{}")
	exePath := filepath.Join(binDir, exeName())
	writeFile(t, exePath, "")
	writeFile(t, filepath.Join(pathDir, defaultRgCommand()), "")

	canonPathDir := canonAbs(t, pathDir)

	ctx := detectFromExeWithCodexHome(false, exePath, true, false, nil)
	if ctx.Method.Kind() != MethodNpm {
		t.Fatalf("expected npm, got %v", ctx.Method.Kind())
	}
	if ctx.PackageLayout == nil {
		t.Fatalf("expected package layout")
	}
	if ctx.RgCommand() != canonPathDir.Join(defaultRgCommand()).Path() {
		t.Fatalf("rg mismatch")
	}
}

func TestStandalonePackageRgFallsBackWhenCodexPathIsMissing(t *testing.T) {
	packageDir := t.TempDir()
	binDir := filepath.Join(packageDir, binDirname)
	mkdirAll(t, binDir)
	writeFile(t, filepath.Join(packageDir, packageMetadataFilename), "{}")
	exePath := filepath.Join(binDir, exeName())
	writeFile(t, exePath, "")

	ctx := detectFromExeWithCodexHome(false, exePath, false, false, nil)
	if ctx.RgCommand() != defaultRgCommand() {
		t.Fatalf("rg got %q want %q", ctx.RgCommand(), defaultRgCommand())
	}
}

func TestBundledFileLookupsIgnoreDirectories(t *testing.T) {
	packageDir := t.TempDir()
	binDir := filepath.Join(packageDir, binDirname)
	resourcesDir := filepath.Join(packageDir, resourcesDirname)
	pathDir := filepath.Join(packageDir, pathDirname)
	mkdirAll(t, binDir)
	mkdirAll(t, filepath.Join(resourcesDir, testResourceName))
	mkdirAll(t, filepath.Join(pathDir, defaultRgCommand()))
	writeFile(t, filepath.Join(packageDir, packageMetadataFilename), "{}")
	exePath := filepath.Join(binDir, exeName())
	writeFile(t, exePath, "")

	ctx := detectFromExeWithCodexHome(false, exePath, false, false, nil)
	if ctx.RgCommand() != defaultRgCommand() {
		t.Fatalf("rg got %q want %q", ctx.RgCommand(), defaultRgCommand())
	}
	if _, ok := ctx.BundledResource(testResourceName); ok {
		t.Fatalf("expected no bundled resource for a directory")
	}
}

func TestNpmAndBunTakePrecedence(t *testing.T) {
	npmCtx := detectFromExeWithCodexHome(false, "/tmp/codex", true, false, nil)
	if npmCtx.Method.Kind() != MethodNpm || npmCtx.PackageLayout != nil {
		t.Fatalf("npm precedence failed: %+v", npmCtx)
	}

	bunCtx := detectFromExeWithCodexHome(false, "/tmp/codex", false, true, nil)
	if bunCtx.Method.Kind() != MethodBun || bunCtx.PackageLayout != nil {
		t.Fatalf("bun precedence failed: %+v", bunCtx)
	}
}

func TestBrewIsDetectedOnMacOSPrefixes(t *testing.T) {
	tests := []struct {
		name string
		exe  string
		want MethodKind
	}{
		{name: "opt homebrew", exe: "/opt/homebrew/bin/codex", want: MethodBrew},
		{name: "usr local", exe: "/usr/local/bin/codex", want: MethodBrew},
		{name: "elsewhere", exe: "/tmp/codex", want: MethodOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := detectFromExeWithCodexHome(true, tt.exe, false, false, nil)
			if ctx.Method.Kind() != tt.want {
				t.Fatalf("got %v want %v", ctx.Method.Kind(), tt.want)
			}
			if ctx.PackageLayout != nil {
				t.Fatalf("expected no package layout")
			}
		})
	}
}

func TestBrewNotDetectedWhenNotMacOS(t *testing.T) {
	ctx := detectFromExeWithCodexHome(false, "/opt/homebrew/bin/codex", false, false, nil)
	if ctx.Method.Kind() != MethodOther {
		t.Fatalf("got %v want Other", ctx.Method.Kind())
	}
}

func TestNoExeIsOther(t *testing.T) {
	ctx := detectFromExeWithCodexHome(false, "", false, false, nil)
	if ctx.Method.Kind() != MethodOther {
		t.Fatalf("got %v want Other", ctx.Method.Kind())
	}
	if ctx.PackageLayout != nil {
		t.Fatalf("expected no package layout")
	}
}

func TestPathStartsWith(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		prefix string
		want   bool
	}{
		{name: "exact", path: "/a/b", prefix: "/a/b", want: true},
		{name: "child", path: "/a/b/c", prefix: "/a/b", want: true},
		{name: "component boundary", path: "/a/bc", prefix: "/a/b", want: false},
		{name: "not prefix", path: "/x/y", prefix: "/a", want: false},
		{name: "longer prefix", path: "/a", prefix: "/a/b", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathStartsWith(tt.path, tt.prefix); got != tt.want {
				t.Fatalf("pathStartsWith(%q,%q)=%v want %v", tt.path, tt.prefix, got, tt.want)
			}
		})
	}
}
