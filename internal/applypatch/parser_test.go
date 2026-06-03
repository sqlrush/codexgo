package applypatch

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// asParseError extracts a *ParseError from err, failing the test otherwise.
func asParseError(t *testing.T, err error) *ParseError {
	t.Helper()
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ParseError, got %T: %v", err, err)
	}
	return pe
}

func TestParseOneHunk(t *testing.T) {
	_, _, err := parseOneHunk([]string{"bad"}, 234)
	pe := asParseError(t, err)
	want := &ParseError{
		Kind:       InvalidHunk,
		LineNumber: 234,
		Message: "'bad' is not a valid hunk header. " +
			"Valid hunk headers: '*** Add File: {path}', '*** Delete File: {path}', '*** Update File: {path}'",
	}
	if !reflect.DeepEqual(pe, want) {
		t.Fatalf("got %#v, want %#v", pe, want)
	}
}

func TestParseUpdateFileChunk(t *testing.T) {
	t.Run("missing context not allowed", func(t *testing.T) {
		_, _, err := parseUpdateFileChunk([]string{"bad"}, 123, false)
		pe := asParseError(t, err)
		if pe.Kind != InvalidHunk || pe.LineNumber != 123 ||
			pe.Message != "Expected update hunk to start with a @@ context marker, got: 'bad'" {
			t.Fatalf("unexpected error: %#v", pe)
		}
	})

	t.Run("empty after marker", func(t *testing.T) {
		_, _, err := parseUpdateFileChunk([]string{"@@"}, 123, false)
		pe := asParseError(t, err)
		if pe.LineNumber != 124 || pe.Message != "Update hunk does not contain any lines" {
			t.Fatalf("unexpected error: %#v", pe)
		}
	})

	t.Run("bad line after marker", func(t *testing.T) {
		_, _, err := parseUpdateFileChunk([]string{"@@", "bad"}, 123, false)
		pe := asParseError(t, err)
		want := "Unexpected line found in update hunk: 'bad'. Every line should start with ' ' (context line), '+' (added line), or '-' (removed line)"
		if pe.LineNumber != 124 || pe.Message != want {
			t.Fatalf("unexpected error: %#v", pe)
		}
	})

	t.Run("eof with no lines", func(t *testing.T) {
		_, _, err := parseUpdateFileChunk([]string{"@@", "*** End of File"}, 123, false)
		pe := asParseError(t, err)
		if pe.LineNumber != 124 || pe.Message != "Update hunk does not contain any lines" {
			t.Fatalf("unexpected error: %#v", pe)
		}
	})

	t.Run("full chunk with context", func(t *testing.T) {
		chunk, consumed, err := parseUpdateFileChunk(
			[]string{
				"@@ change_context",
				"",
				" context",
				"-remove",
				"+add",
				" context2",
				"*** End Patch",
			},
			123, false,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := UpdateFileChunk{
			ChangeContext:    "change_context",
			HasChangeContext: true,
			OldLines:         []string{"", "context", "remove", "context2"},
			NewLines:         []string{"", "context", "add", "context2"},
			IsEndOfFile:      false,
		}
		if !reflect.DeepEqual(chunk, want) {
			t.Fatalf("chunk got %#v, want %#v", chunk, want)
		}
		if consumed != 6 {
			t.Fatalf("consumed got %d, want 6", consumed)
		}
	})

	t.Run("eof chunk no context", func(t *testing.T) {
		chunk, consumed, err := parseUpdateFileChunk(
			[]string{"@@", "+line", "*** End of File"}, 123, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := UpdateFileChunk{
			ChangeContext:    "",
			HasChangeContext: false,
			OldLines:         nil,
			NewLines:         []string{"line"},
			IsEndOfFile:      true,
		}
		if !reflect.DeepEqual(chunk, want) {
			t.Fatalf("chunk got %#v, want %#v", chunk, want)
		}
		if consumed != 3 {
			t.Fatalf("consumed got %d, want 3", consumed)
		}
	})
}

func TestParsePatch(t *testing.T) {
	t.Run("bad first line", func(t *testing.T) {
		_, err := parsePatchText("bad", parseModeStrict)
		pe := asParseError(t, err)
		if pe.Kind != InvalidPatch || pe.Message != "The first line of the patch must be '*** Begin Patch'" {
			t.Fatalf("unexpected error: %#v", pe)
		}
	})

	t.Run("bad last line", func(t *testing.T) {
		_, err := parsePatchText("*** Begin Patch\nbad", parseModeStrict)
		pe := asParseError(t, err)
		if pe.Message != "The last line of the patch must be '*** End Patch'" {
			t.Fatalf("unexpected error: %#v", pe)
		}
	})

	t.Run("add file with surrounding whitespace markers", func(t *testing.T) {
		res, err := parsePatchText(
			"*** Begin Patch \n*** Add File: foo\n+hi\n *** End Patch",
			parseModeStrict,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []Hunk{{Kind: HunkAddFile, Path: "foo", Contents: "hi\n"}}
		if !reflect.DeepEqual(res.Hunks, want) {
			t.Fatalf("hunks got %#v, want %#v", res.Hunks, want)
		}
	})

	t.Run("empty update hunk", func(t *testing.T) {
		_, err := parsePatchText(
			"*** Begin Patch\n*** Update File: test.py\n*** End Patch",
			parseModeStrict,
		)
		pe := asParseError(t, err)
		if pe.Kind != InvalidHunk || pe.LineNumber != 2 ||
			pe.Message != "Update file hunk for path 'test.py' is empty" {
			t.Fatalf("unexpected error: %#v", pe)
		}
	})

	t.Run("empty patch yields no hunks", func(t *testing.T) {
		res, err := parsePatchText("*** Begin Patch\n*** End Patch", parseModeStrict)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res.Hunks) != 0 {
			t.Fatalf("expected no hunks, got %#v", res.Hunks)
		}
	})

	t.Run("multiple hunk kinds", func(t *testing.T) {
		patch := "*** Begin Patch\n" +
			"*** Add File: path/add.py\n" +
			"+abc\n" +
			"+def\n" +
			"*** Delete File: path/delete.py\n" +
			"*** Update File: path/update.py\n" +
			"*** Move to: path/update2.py\n" +
			"@@ def f():\n" +
			"-    pass\n" +
			"+    return 123\n" +
			"*** End Patch"
		res, err := parsePatchText(patch, parseModeStrict)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []Hunk{
			{Kind: HunkAddFile, Path: "path/add.py", Contents: "abc\ndef\n"},
			{Kind: HunkDeleteFile, Path: "path/delete.py"},
			{
				Kind:        HunkUpdateFile,
				Path:        "path/update.py",
				MovePath:    "path/update2.py",
				HasMovePath: true,
				Chunks: []UpdateFileChunk{{
					ChangeContext:    "def f():",
					HasChangeContext: true,
					OldLines:         []string{"    pass"},
					NewLines:         []string{"    return 123"},
				}},
			},
		}
		if !reflect.DeepEqual(res.Hunks, want) {
			t.Fatalf("hunks got %#v, want %#v", res.Hunks, want)
		}
	})

	t.Run("update hunk followed by add", func(t *testing.T) {
		patch := "*** Begin Patch\n" +
			"*** Update File: file.py\n" +
			"@@\n" +
			"+line\n" +
			"*** Add File: other.py\n" +
			"+content\n" +
			"*** End Patch"
		res, err := parsePatchText(patch, parseModeStrict)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []Hunk{
			{
				Kind: HunkUpdateFile,
				Path: "file.py",
				Chunks: []UpdateFileChunk{{
					HasChangeContext: false,
					OldLines:         nil,
					NewLines:         []string{"line"},
				}},
			},
			{Kind: HunkAddFile, Path: "other.py", Contents: "content\n"},
		}
		if !reflect.DeepEqual(res.Hunks, want) {
			t.Fatalf("hunks got %#v, want %#v", res.Hunks, want)
		}
	})

	t.Run("update hunk without explicit @@ first chunk", func(t *testing.T) {
		patch := "*** Begin Patch\n" +
			"*** Update File: file2.py\n" +
			" import foo\n" +
			"+bar\n" +
			"*** End Patch"
		res, err := parsePatchText(patch, parseModeStrict)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []Hunk{{
			Kind: HunkUpdateFile,
			Path: "file2.py",
			Chunks: []UpdateFileChunk{{
				HasChangeContext: false,
				OldLines:         []string{"import foo"},
				NewLines:         []string{"import foo", "bar"},
			}},
		}}
		if !reflect.DeepEqual(res.Hunks, want) {
			t.Fatalf("hunks got %#v, want %#v", res.Hunks, want)
		}
	})
}

func TestParsePatchAcceptsRelativeAndAbsoluteHunkPaths(t *testing.T) {
	dir := t.TempDir()
	absoluteDelete := filepath.Join(dir, "absolute-delete.py")
	absoluteUpdate := filepath.Join(dir, "absolute-update.py")
	patch := fmt.Sprintf("*** Begin Patch\n"+
		"*** Add File: relative-add.py\n"+
		"+content\n"+
		"*** Delete File: %s\n"+
		"*** Update File: %s\n"+
		"@@\n"+
		"-old\n"+
		"+new\n"+
		"*** End Patch", absoluteDelete, absoluteUpdate)

	res, err := parsePatchText(patch, parseModeStrict)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []Hunk{
		{Kind: HunkAddFile, Path: "relative-add.py", Contents: "content\n"},
		{Kind: HunkDeleteFile, Path: absoluteDelete},
		{
			Kind: HunkUpdateFile,
			Path: absoluteUpdate,
			Chunks: []UpdateFileChunk{{
				OldLines: []string{"old"},
				NewLines: []string{"new"},
			}},
		},
	}
	if !reflect.DeepEqual(res.Hunks, want) {
		t.Fatalf("hunks got %#v, want %#v", res.Hunks, want)
	}
}

func TestHunkResolvePathAcceptsRelativeAndAbsolutePaths(t *testing.T) {
	cwdDir := t.TempDir()
	cwd, err := abspath.FromAbsolutePath(cwdDir)
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}
	absoluteDir := t.TempDir()
	absoluteAdd := filepath.Join(absoluteDir, "absolute-add.py")
	absoluteDelete := filepath.Join(absoluteDir, "absolute-delete.py")
	absoluteUpdate := filepath.Join(absoluteDir, "absolute-update.py")

	cases := []struct {
		hunk Hunk
		want string
	}{
		{Hunk{Kind: HunkAddFile, Path: "relative-add.py"}, filepath.Join(cwdDir, "relative-add.py")},
		{Hunk{Kind: HunkDeleteFile, Path: "relative-delete.py"}, filepath.Join(cwdDir, "relative-delete.py")},
		{Hunk{Kind: HunkUpdateFile, Path: "relative-update.py"}, filepath.Join(cwdDir, "relative-update.py")},
		{Hunk{Kind: HunkAddFile, Path: absoluteAdd}, absoluteAdd},
		{Hunk{Kind: HunkDeleteFile, Path: absoluteDelete}, absoluteDelete},
		{Hunk{Kind: HunkUpdateFile, Path: absoluteUpdate}, absoluteUpdate},
	}
	for _, c := range cases {
		got := c.hunk.ResolvePath(cwd).Path()
		if got != c.want {
			t.Errorf("ResolvePath(%q) = %q, want %q", c.hunk.Path, got, c.want)
		}
	}
}

func TestParsePatchLenient(t *testing.T) {
	patchText := "*** Begin Patch\n" +
		"*** Update File: file2.py\n" +
		" import foo\n" +
		"+bar\n" +
		"*** End Patch"
	expectedHunks := []Hunk{{
		Kind: HunkUpdateFile,
		Path: "file2.py",
		Chunks: []UpdateFileChunk{{
			OldLines: []string{"import foo"},
			NewLines: []string{"import foo", "bar"},
		}},
	}}
	const firstLineErr = "The first line of the patch must be '*** Begin Patch'"
	const lastLineErr = "The last line of the patch must be '*** End Patch'"

	checkErr := func(t *testing.T, err error, msg string) {
		t.Helper()
		pe := asParseError(t, err)
		if pe.Message != msg {
			t.Fatalf("got message %q, want %q", pe.Message, msg)
		}
	}
	checkOK := func(t *testing.T, res *ParseResult, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(res.Hunks, expectedHunks) {
			t.Fatalf("hunks got %#v, want %#v", res.Hunks, expectedHunks)
		}
		if res.Patch != patchText {
			t.Fatalf("patch got %q, want %q", res.Patch, patchText)
		}
		if res.EnvironmentID != nil {
			t.Fatalf("expected nil environment id, got %v", *res.EnvironmentID)
		}
	}

	for _, heredoc := range []string{"<<EOF", "<<'EOF'", "<<\"EOF\""} {
		wrapped := heredoc + "\n" + patchText + "\nEOF\n"

		_, errStrict := parsePatchText(wrapped, parseModeStrict)
		checkErr(t, errStrict, firstLineErr)

		res, errLenient := parsePatchText(wrapped, parseModeLenient)
		checkOK(t, res, errLenient)
	}

	// Mismatched quotes: not a recognized heredoc, so it fails in both modes.
	mismatched := "<<\"EOF'\n" + patchText + "\nEOF\n"
	_, errStrict := parsePatchText(mismatched, parseModeStrict)
	checkErr(t, errStrict, firstLineErr)
	_, errLenient := parsePatchText(mismatched, parseModeLenient)
	checkErr(t, errLenient, firstLineErr)

	// Missing closing heredoc: strict fails on first line; lenient strips the
	// wrapper and then fails on the last line.
	missingClose := "<<EOF\n*** Begin Patch\n*** Update File: file2.py\nEOF\n"
	_, errStrict2 := parsePatchText(missingClose, parseModeStrict)
	checkErr(t, errStrict2, firstLineErr)
	_, errLenient2 := parsePatchText(missingClose, parseModeLenient)
	checkErr(t, errLenient2, lastLineErr)
}

func TestParsePatchEnvironmentIDPreamble(t *testing.T) {
	t.Run("valid environment id", func(t *testing.T) {
		patch := "*** Begin Patch\n" +
			"*** Environment ID: remote\n" +
			"*** Add File: hello.txt\n" +
			"+hello\n" +
			"*** End Patch"
		res, err := parsePatchText(patch, parseModeStrict)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantHunks := []Hunk{{Kind: HunkAddFile, Path: "hello.txt", Contents: "hello\n"}}
		if !reflect.DeepEqual(res.Hunks, wantHunks) {
			t.Fatalf("hunks got %#v, want %#v", res.Hunks, wantHunks)
		}
		wantPatch := "*** Begin Patch\n*** Environment ID: remote\n*** Add File: hello.txt\n+hello\n*** End Patch"
		if res.Patch != wantPatch {
			t.Fatalf("patch got %q, want %q", res.Patch, wantPatch)
		}
		if res.EnvironmentID == nil || *res.EnvironmentID != "remote" {
			t.Fatalf("environment id got %v, want remote", res.EnvironmentID)
		}
	})

	t.Run("empty environment id", func(t *testing.T) {
		patch := "*** Begin Patch\n" +
			"*** Environment ID:   \n" +
			"*** Add File: hello.txt\n" +
			"+hello\n" +
			"*** End Patch"
		_, err := parsePatchText(patch, parseModeStrict)
		pe := asParseError(t, err)
		if pe.Kind != InvalidPatch || pe.Message != "apply_patch environment_id cannot be empty" {
			t.Fatalf("unexpected error: %#v", pe)
		}
	})
}

// Ensure the package's exported ParsePatch (lenient by default) parses a basic
// envelope.
func TestParsePatchPublicEntrypoint(t *testing.T) {
	res, err := ParsePatch("*** Begin Patch\n*** Add File: x.txt\n+hi\n*** End Patch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []Hunk{{Kind: HunkAddFile, Path: "x.txt", Contents: "hi\n"}}
	if !reflect.DeepEqual(res.Hunks, want) {
		t.Fatalf("hunks got %#v, want %#v", res.Hunks, want)
	}
}
