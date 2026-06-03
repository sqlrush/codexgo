package plugins

// Small filesystem helpers shared across the plugin subsystem.

import (
	"os"
)

// readFileString reads a file fully and returns its contents as a string,
// wrapping the os error.
func readFileString(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// isDir reports whether path exists and is a directory.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// isFile reports whether path exists and is a regular file.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// pathExists reports whether path exists (any type).
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
