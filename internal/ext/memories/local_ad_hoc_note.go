package memories

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// adHocNotesDir is the relative directory under the memory root that holds
// append-only ad-hoc notes, mirroring AD_HOC_NOTES_DIR.
var adHocNotesDir = []string{"extensions", "ad_hoc", "notes"}

const (
	adHocNoteFilenameMaxBytes = 128
	adHocNoteSlugMaxBytes     = 80
	// timestampPrefixLen is the byte length of "YYYY-MM-DDTHH-MM-SS-".
	timestampPrefixLen = len("YYYY-MM-DDTHH-MM-SS-")
)

// addAdHocNote creates a single append-only ad-hoc note file, mirroring
// local::ad_hoc_note::add_ad_hoc_note.
func (b *LocalBackend) addAdHocNote(_ context.Context, req AddAdHocNoteRequest) (AddAdHocNoteResponse, error) {
	if err := validateFilename(req.Filename); err != nil {
		return AddAdHocNoteResponse{}, err
	}
	if strings.TrimSpace(req.Note) == "" {
		return AddAdHocNoteResponse{}, errEmptyAdHocNote()
	}

	notesDir, err := b.ensureNotesDir()
	if err != nil {
		return AddAdHocNoteResponse{}, err
	}
	path := filepath.Join(notesDir, req.Filename)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return AddAdHocNoteResponse{}, errAdHocNoteAlreadyExists(req.Filename)
		}
		return AddAdHocNoteResponse{}, errIO(err)
	}
	if _, writeErr := file.WriteString(req.Note); writeErr != nil {
		_ = file.Close()
		return AddAdHocNoteResponse{}, errIO(writeErr)
	}
	if closeErr := file.Close(); closeErr != nil {
		return AddAdHocNoteResponse{}, errIO(closeErr)
	}
	return AddAdHocNoteResponse{}, nil
}

// ensureNotesDir creates the ad-hoc notes directory chain, validating that each
// level is a real (non-symlink) directory, mirroring ensure_notes_dir.
func (b *LocalBackend) ensureNotesDir() (string, error) {
	if err := ensureDirectory(b.root); err != nil {
		return "", err
	}
	path := b.root
	for _, component := range adHocNotesDir {
		path = filepath.Join(path, component)
		if err := ensureDirectory(path); err != nil {
			return "", err
		}
	}
	return path, nil
}

// ensureDirectory ensures path is a real directory, creating it when missing,
// mirroring ensure_directory.
func ensureDirectory(path string) error {
	info, ok, err := metadataOrNone(path)
	if err != nil {
		return err
	}
	if ok {
		if symErr := rejectSymlink(path, info); symErr != nil {
			return symErr
		}
		if info.IsDir() {
			return nil
		}
		return errInvalidPath(path, "must be a directory")
	}

	if mkErr := os.Mkdir(path, 0o755); mkErr != nil {
		return errIO(mkErr)
	}

	info, ok, err = metadataOrNone(path)
	if err != nil {
		return err
	}
	if !ok {
		return errNotFound(path)
	}
	if symErr := rejectSymlink(path, info); symErr != nil {
		return symErr
	}
	if !info.IsDir() {
		return errInvalidPath(path, "must be a directory")
	}
	return nil
}

// validateFilename enforces the YYYY-MM-DDTHH-MM-SS-<slug>.md naming rules,
// mirroring validate_filename.
func validateFilename(filename string) error {
	if len(filename) > adHocNoteFilenameMaxBytes {
		return errInvalidFilename(filename, "must be at most 128 bytes")
	}
	stem, ok := strings.CutSuffix(filename, ".md")
	if !ok {
		return errInvalidFilename(filename, "must end with .md")
	}
	if len(stem) < timestampPrefixLen {
		return errInvalidFilename(filename, "must use YYYY-MM-DDTHH-MM-SS-<slug>.md")
	}
	slug := stem[timestampPrefixLen:]
	if !hasValidTimestampPrefix(stem) {
		return errInvalidFilename(filename, "must use YYYY-MM-DDTHH-MM-SS-<slug>.md")
	}
	if len(slug) == 0 || len(slug) > adHocNoteSlugMaxBytes {
		return errInvalidFilename(filename, "slug must be 1 to 80 bytes")
	}
	for i := 0; i < len(slug); i++ {
		c := slug[i]
		if !(isASCIILower(c) || isASCIIDigit(c) || c == '-') {
			return errInvalidFilename(filename, "slug must contain only lowercase ASCII letters, digits, or hyphens")
		}
	}
	return nil
}

// hasValidTimestampPrefix validates the fixed-width YYYY-MM-DDTHH-MM-SS- prefix,
// mirroring has_valid_timestamp_prefix.
func hasValidTimestampPrefix(stem string) bool {
	b := []byte(stem)
	return len(b) > timestampPrefixLen &&
		b[4] == '-' &&
		b[7] == '-' &&
		b[10] == 'T' &&
		b[13] == '-' &&
		b[16] == '-' &&
		b[19] == '-' &&
		areDigits(b[0:4]) &&
		areDigits(b[5:7]) &&
		areDigits(b[8:10]) &&
		areDigits(b[11:13]) &&
		areDigits(b[14:16]) &&
		areDigits(b[17:19])
}

func areDigits(b []byte) bool {
	for _, c := range b {
		if !isASCIIDigit(c) {
			return false
		}
	}
	return true
}

func isASCIILower(c byte) bool { return c >= 'a' && c <= 'z' }
func isASCIIDigit(c byte) bool { return c >= '0' && c <= '9' }
