package plugins

// tar.gz plugin bundle packing/unpacking with path-traversal and size guards.
// Ports `codex-rs/core-plugins/src/plugin_bundle_archive.rs`.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrArchiveSizeLimitExceeded is the sentinel reported when packing exceeds the
// configured byte budget.
var ErrArchiveSizeLimitExceeded = errors.New("archive size limit exceeded")

// PluginBundlePackError mirrors the Rust `PluginBundlePackError` variants.
type PluginBundlePackError struct {
	Message string
	Err     error
}

func (e *PluginBundlePackError) Error() string { return e.Message }
func (e *PluginBundlePackError) Unwrap() error { return e.Err }

// PluginBundleUnpackError mirrors the Rust `PluginBundleUnpackError` variants.
type PluginBundleUnpackError struct {
	Message string
	Err     error
}

func (e *PluginBundleUnpackError) Error() string { return e.Message }
func (e *PluginBundleUnpackError) Unwrap() error { return e.Err }

func unpackIOError(context string, err error) *PluginBundleUnpackError {
	return &PluginBundleUnpackError{Message: fmt.Sprintf("%s: %s", context, err.Error()), Err: err}
}

// PackPluginBundleTarGz mirrors the Rust `pack_plugin_bundle_tar_gz`.
//
// It archives pluginPath (which must be a directory containing
// ".codex-plugin/plugin.json") as a gzip-compressed tar, enforcing maxBytes on
// the compressed output. Entries are emitted in sorted order for deterministic,
// reproducible archives.
func PackPluginBundleTarGz(pluginPath string, maxBytes int) ([]byte, error) {
	if !isDir(pluginPath) {
		return nil, &PluginBundlePackError{Message: fmt.Sprintf(
			"invalid plugin path `%s`: expected a plugin directory", pluginPath)}
	}
	if !isFile(filepath.Join(pluginPath, ".codex-plugin", "plugin.json")) {
		return nil, &PluginBundlePackError{Message: fmt.Sprintf(
			"invalid plugin path `%s`: missing .codex-plugin/plugin.json", pluginPath)}
	}

	buf := newSizeLimitedBuffer(maxBytes)
	gzWriter := gzip.NewWriter(buf)
	tarWriter := tar.NewWriter(gzWriter)

	if err := appendPluginTree(tarWriter, pluginPath, pluginPath); err != nil {
		return nil, packError(err, maxBytes)
	}
	if err := tarWriter.Close(); err != nil {
		return nil, packError(err, maxBytes)
	}
	if err := gzWriter.Close(); err != nil {
		return nil, packError(err, maxBytes)
	}
	return buf.intoInner(), nil
}

func packError(err error, maxBytes int) error {
	if errors.Is(err, ErrArchiveSizeLimitExceeded) {
		var limit *sizeLimitExceeded
		if errors.As(err, &limit) {
			return &PluginBundlePackError{Message: fmt.Sprintf(
				"plugin archive would be %d bytes, exceeding maximum size of %d bytes",
				limit.bytes, limit.maxBytes)}
		}
		return &PluginBundlePackError{Message: fmt.Sprintf(
			"plugin archive exceeds maximum size of %d bytes", maxBytes)}
	}
	return &PluginBundlePackError{Message: fmt.Sprintf("failed to archive plugin bundle: %s", err.Error()), Err: err}
}

// appendPluginTree mirrors the Rust `append_plugin_tree`: it walks current
// (relative to pluginRoot) in sorted order, writing directory and regular-file
// entries and rejecting other entry types.
func appendPluginTree(tw *tar.Writer, pluginRoot, current string) error {
	entries, err := os.ReadDir(current)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		path := filepath.Join(current, entry.Name())
		relative, err := filepath.Rel(pluginRoot, path)
		if err != nil {
			return fmt.Errorf("failed to compute plugin archive path for `%s`: %w", path, err)
		}
		relSlash := filepath.ToSlash(relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			hdr := &tar.Header{
				Name:     relSlash + "/",
				Typeflag: tar.TypeDir,
				Mode:     int64(info.Mode().Perm()),
			}
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			if err := appendPluginTree(tw, pluginRoot, path); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			hdr := &tar.Header{
				Name:     relSlash,
				Typeflag: tar.TypeReg,
				Mode:     int64(info.Mode().Perm()),
				Size:     int64(len(data)),
			}
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			if _, err := tw.Write(data); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported plugin archive entry type: %s", path)
		}
	}
	return nil
}

// UnpackPluginBundleTarGz mirrors the Rust `unpack_plugin_bundle_tar_gz`.
//
// It extracts a gzip-compressed tar bundle into destination, enforcing
// maxTotalBytes on the cumulative size of extracted files and rejecting entries
// that escape the destination, links, and unsupported entry types.
func UnpackPluginBundleTarGz(data []byte, destination string, maxTotalBytes uint64) error {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return unpackIOError("failed to create plugin bundle extraction directory", err)
	}
	gzReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return unpackIOError("failed to read plugin bundle tar", err)
	}
	defer gzReader.Close()
	return unpackPluginBundleTar(tar.NewReader(gzReader), destination, maxTotalBytes)
}

func unpackPluginBundleTar(tr *tar.Reader, destination string, maxTotalBytes uint64) error {
	var extractedBytes uint64
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return unpackIOError("failed to read plugin bundle tar entry", err)
		}

		entryName := filepath.FromSlash(header.Name)
		outputPath, err := checkedTarOutputPath(destination, entryName)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(outputPath, 0o755); err != nil {
				return unpackIOError("failed to create plugin bundle directory", err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := enforceTotalExtractedSize(uint64(header.Size), &extractedBytes, maxTotalBytes); err != nil {
				return err
			}
			parent := filepath.Dir(outputPath)
			if parent == "" {
				return &PluginBundleUnpackError{Message: fmt.Sprintf(
					"plugin bundle output path has no parent: %s", outputPath)}
			}
			if err := os.MkdirAll(parent, 0o755); err != nil {
				return unpackIOError("failed to create plugin bundle directory", err)
			}
			if err := writeTarFile(tr, outputPath, header); err != nil {
				return err
			}
		case tar.TypeLink, tar.TypeSymlink:
			return &PluginBundleUnpackError{Message: fmt.Sprintf(
				"plugin bundle tar entry `%s` is a link", header.Name)}
		default:
			return &PluginBundleUnpackError{Message: fmt.Sprintf(
				"plugin bundle tar entry `%s` has unsupported type %v", header.Name, header.Typeflag)}
		}
	}
	return nil
}

func writeTarFile(tr *tar.Reader, outputPath string, header *tar.Header) error {
	mode := os.FileMode(header.Mode).Perm()
	if mode == 0 {
		mode = 0o644
	}
	file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return unpackIOError("failed to unpack plugin bundle entry", err)
	}
	defer file.Close()
	if _, err := io.Copy(file, tr); err != nil {
		return unpackIOError("failed to unpack plugin bundle entry", err)
	}
	return nil
}

// checkedTarOutputPath mirrors the Rust `checked_tar_output_path`: it joins only
// normal components onto destination, rejecting "..", absolute and prefix
// components and empty paths.
func checkedTarOutputPath(destination, entryName string) (string, error) {
	outputPath := destination
	hasComponent := false
	cleaned := filepath.ToSlash(entryName)
	for _, segment := range strings.Split(cleaned, "/") {
		switch segment {
		case "", ".":
			// Empty and current-dir components are skipped.
			continue
		case "..":
			return "", &PluginBundleUnpackError{Message: fmt.Sprintf(
				"plugin bundle tar entry `%s` escapes extraction root", entryName)}
		default:
			if filepath.IsAbs(segment) || filepath.VolumeName(segment) != "" {
				return "", &PluginBundleUnpackError{Message: fmt.Sprintf(
					"plugin bundle tar entry `%s` escapes extraction root", entryName)}
			}
			hasComponent = true
			outputPath = filepath.Join(outputPath, segment)
		}
	}
	if !hasComponent {
		return "", &PluginBundleUnpackError{Message: "plugin bundle tar entry has an empty path"}
	}
	return outputPath, nil
}

// enforceTotalExtractedSize mirrors the Rust `enforce_total_extracted_size`.
func enforceTotalExtractedSize(entrySize uint64, extractedBytes *uint64, maxTotalBytes uint64) error {
	next := *extractedBytes + entrySize
	if next < *extractedBytes { // overflow
		return &PluginBundleUnpackError{Message: fmt.Sprintf(
			"plugin bundle extracted size would be %d bytes, exceeding maximum total size of %d bytes",
			^uint64(0), maxTotalBytes)}
	}
	if next > maxTotalBytes {
		return &PluginBundleUnpackError{Message: fmt.Sprintf(
			"plugin bundle extracted size would be %d bytes, exceeding maximum total size of %d bytes",
			next, maxTotalBytes)}
	}
	*extractedBytes = next
	return nil
}

// sizeLimitExceeded carries the over-limit byte counts for pack errors.
type sizeLimitExceeded struct {
	bytes    int
	maxBytes int
}

func (e *sizeLimitExceeded) Error() string {
	return fmt.Sprintf("archive would be %d bytes, exceeding maximum size of %d bytes", e.bytes, e.maxBytes)
}

func (e *sizeLimitExceeded) Unwrap() error { return ErrArchiveSizeLimitExceeded }

// sizeLimitedBuffer mirrors the Rust `SizeLimitedBuffer`: an io.Writer that
// fails once its accumulated size would exceed maxBytes.
type sizeLimitedBuffer struct {
	bytes    []byte
	maxBytes int
}

func newSizeLimitedBuffer(maxBytes int) *sizeLimitedBuffer {
	return &sizeLimitedBuffer{maxBytes: maxBytes}
}

func (b *sizeLimitedBuffer) Write(p []byte) (int, error) {
	next := len(b.bytes) + len(p)
	if next < len(b.bytes) { // overflow
		return 0, &sizeLimitExceeded{bytes: int(^uint(0) >> 1), maxBytes: b.maxBytes}
	}
	if next > b.maxBytes {
		return 0, &sizeLimitExceeded{bytes: next, maxBytes: b.maxBytes}
	}
	b.bytes = append(b.bytes, p...)
	return len(p), nil
}

func (b *sizeLimitedBuffer) intoInner() []byte {
	return b.bytes
}
