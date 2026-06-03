package config

import (
	"bytes"
	"io"
	"path/filepath"
)

func bytesReader(data []byte) io.Reader {
	return bytes.NewReader(data)
}

// isAbsPath reports whether the path is absolute on the current platform.
func isAbsPath(p string) bool {
	return filepath.IsAbs(p)
}
