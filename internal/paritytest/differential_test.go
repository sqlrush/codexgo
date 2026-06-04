package paritytest

// Differential parity tests: compare codexgo's observable behavior against a real
// codex 0.136.0 binary. They are env-gated on CODEX_PARITY_BIN (path to the real
// `codex` binary) and skip when it is unset, so CI stays hermetic. Run locally:
//
//	CODEX_PARITY_BIN=/path/to/codex go test ./internal/paritytest/ -run Parity -v
//
// These encode the manual differential validations recorded in docs/PARITY.md.

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const parityBinEnv = "CODEX_PARITY_BIN"

// referenceBin returns the real codex binary path, skipping the test when unset.
func referenceBin(t *testing.T) string {
	t.Helper()
	bin := os.Getenv(parityBinEnv)
	if bin == "" {
		t.Skipf("%s not set; skipping differential parity tests", parityBinEnv)
	}
	abs, err := filepath.Abs(bin)
	if err != nil {
		t.Fatalf("resolve %s: %v", bin, err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Skipf("%s=%s not found: %v", parityBinEnv, abs, err)
	}
	return abs
}

// moduleRoot walks up from the working directory to the repo root (the dir with
// go.mod).
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above working directory")
		}
		dir = parent
	}
}

// buildCodexgo compiles the codexgo `codex` binary into a temp dir once.
func buildCodexgo(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "codexgo")
	cmd := exec.Command("go", "build", "-o", out, "./cmd/codex")
	cmd.Dir = moduleRoot(t)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build codexgo: %v\n%s", err, b)
	}
	return out
}

// runCmd runs bin with args in dir, returning combined output and exit code.
func runCmd(dir, bin string, args ...string) (string, int) {
	cmd := exec.Command(bin, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	switch e := err.(type) {
	case nil:
		return buf.String(), 0
	case *exec.ExitError:
		return buf.String(), e.ExitCode()
	default:
		return buf.String(), -1
	}
}

// helpSubcommands extracts the lowercase subcommand names from the "Commands:"
// section of a --help dump (robust to the surrounding layout differing between
// codex and codexgo). It reads indented lines after a "Commands:" header until a
// blank line or the next section header, taking the first token of each (stripping
// a trailing comma so "exec, e"-style alias lines yield the canonical name).
func helpSubcommands(help string) []string {
	seen := map[string]bool{}
	inCommands := false
	for _, line := range strings.Split(help, "\n") {
		if !inCommands {
			if strings.TrimSpace(line) == "Commands:" {
				inCommands = true
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			break // blank line ends the Commands section
		}
		// Command entries are indented exactly two spaces; wrapped description
		// continuation lines are indented more deeply, so skip them.
		if !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "   ") {
			continue
		}
		name := strings.TrimSuffix(strings.Fields(line)[0], ",")
		if name == "" || name[0] < 'a' || name[0] > 'z' {
			continue
		}
		if strings.Trim(name, "abcdefghijklmnopqrstuvwxyz-") != "" {
			continue
		}
		seen[name] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// modelSlugs extracts the sorted model slug set from a `debug models` JSON dump.
func modelSlugs(t *testing.T, raw string) []string {
	t.Helper()
	var doc struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("parse models json: %v\n%.200s", err, raw)
	}
	out := make([]string, 0, len(doc.Models))
	for _, m := range doc.Models {
		out = append(out, m.Slug)
	}
	sort.Strings(out)
	return out
}

// TestParitySubcommandSet asserts codexgo's top-level subcommand set matches codex.
func TestParitySubcommandSet(t *testing.T) {
	ref := referenceBin(t)
	cgo := buildCodexgo(t)
	refHelp, _ := runCmd("", ref, "--help")
	cgoHelp, _ := runCmd("", cgo, "--help")
	want := helpSubcommands(refHelp)
	got := helpSubcommands(cgoHelp)
	if strings.Join(want, ",") != strings.Join(got, ",") {
		t.Errorf("subcommand set mismatch\n codex:   %v\n codexgo: %v", want, got)
	}
}

// TestParityModelCatalog asserts codexgo's bundled model-slug set matches codex.
func TestParityModelCatalog(t *testing.T) {
	ref := referenceBin(t)
	cgo := buildCodexgo(t)
	refOut, _ := runCmd("", ref, "debug", "models", "--bundled")
	cgoOut, _ := runCmd("", cgo, "debug", "models")
	want := modelSlugs(t, refOut)
	got := modelSlugs(t, cgoOut)
	if strings.Join(want, ",") != strings.Join(got, ",") {
		t.Errorf("model slug set mismatch\n codex:   %v\n codexgo: %v", want, got)
	}
}

// TestParityApplyPatch asserts codex and codexgo apply an identical patch to
// byte-identical results (the most fidelity-critical surface).
func TestParityApplyPatch(t *testing.T) {
	ref := referenceBin(t)
	cgo := buildCodexgo(t)

	patch := strings.Join([]string{
		"*** Begin Patch",
		"*** Update File: foo.txt",
		"@@",
		" line one",
		"-old middle",
		"+new middle",
		" line three",
		"*** Add File: baz.txt",
		"+hello",
		"+world",
		"*** End Patch",
	}, "\n")

	// applyVia symlinks bin as `apply_patch` (arg0 dispatch), seeds an identical
	// workdir, applies the patch, and returns the resulting foo.txt + baz.txt.
	applyVia := func(bin string) (string, string, int) {
		base := t.TempDir()
		linkDir := filepath.Join(base, "bin")
		work := filepath.Join(base, "work")
		_ = os.MkdirAll(linkDir, 0o755)
		_ = os.MkdirAll(work, 0o755)
		link := filepath.Join(linkDir, "apply_patch")
		if err := os.Symlink(bin, link); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		if err := os.WriteFile(filepath.Join(work, "foo.txt"),
			[]byte("line one\nold middle\nline three\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, code := runCmd(work, link, patch)
		foo, _ := os.ReadFile(filepath.Join(work, "foo.txt"))
		baz, _ := os.ReadFile(filepath.Join(work, "baz.txt"))
		return string(foo), string(baz), code
	}

	refFoo, refBaz, refCode := applyVia(ref)
	cgoFoo, cgoBaz, cgoCode := applyVia(cgo)

	if refCode != 0 || cgoCode != 0 {
		t.Fatalf("apply_patch exit codes: codex=%d codexgo=%d", refCode, cgoCode)
	}
	if refFoo != cgoFoo {
		t.Errorf("foo.txt mismatch\n codex:   %q\n codexgo: %q", refFoo, cgoFoo)
	}
	if refBaz != cgoBaz {
		t.Errorf("baz.txt mismatch\n codex:   %q\n codexgo: %q", refBaz, cgoBaz)
	}
}
