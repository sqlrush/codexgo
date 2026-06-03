package cloudreq

import (
	"fmt"
	"strings"
)

// LoadErrorCode categorizes a cloud-requirements load failure. It mirrors the
// Rust `CloudRequirementsLoadErrorCode`.
type LoadErrorCode int

const (
	// LoadErrorTimeout indicates the fetch timed out.
	LoadErrorTimeout LoadErrorCode = iota
	// LoadErrorAuth indicates an unrecoverable auth failure.
	LoadErrorAuth
	// LoadErrorParse indicates the requirements TOML failed to parse.
	LoadErrorParse
	// LoadErrorRequestFailed indicates retries were exhausted.
	LoadErrorRequestFailed
	// LoadErrorInternal indicates an internal/unexpected failure.
	LoadErrorInternal
)

// LoadError is a cloud-requirements load failure. It mirrors the Rust
// `CloudRequirementsLoadError`.
type LoadError struct {
	// Code categorizes the failure.
	Code LoadErrorCode
	// StatusCode is the HTTP status code that caused the failure, when known.
	StatusCode *int
	// Message is the user-facing message.
	Message string
}

// Error implements error.
func (e *LoadError) Error() string { return e.Message }

// newLoadError constructs a LoadError.
func newLoadError(code LoadErrorCode, statusCode *int, message string) *LoadError {
	return &LoadError{Code: code, StatusCode: statusCode, Message: message}
}

// cacheLoadStatus categorizes why a cache read did not yield a usable payload.
// It mirrors the Rust `CacheLoadStatus`.
type cacheLoadStatus int

const (
	cacheAuthIdentityIncomplete cacheLoadStatus = iota
	cacheFileNotFound
	cacheReadFailed
	cacheParseFailed
	cacheSignatureInvalid
	cacheIdentityIncomplete
	cacheIdentityMismatch
	cacheExpired
)

// cacheLoadError pairs a cache-load status with an optional detail.
type cacheLoadError struct {
	status cacheLoadStatus
	detail string
}

func (e *cacheLoadError) Error() string {
	switch e.status {
	case cacheAuthIdentityIncomplete:
		return "Skipping cloud requirements cache read because auth identity is incomplete."
	case cacheFileNotFound:
		return "Cloud requirements cache file not found."
	case cacheReadFailed:
		return fmt.Sprintf("Failed to read cloud requirements cache: %s.", e.detail)
	case cacheParseFailed:
		return fmt.Sprintf("Failed to parse cloud requirements cache: %s.", e.detail)
	case cacheSignatureInvalid:
		return "Cloud requirements cache failed signature verification."
	case cacheIdentityIncomplete:
		return "Ignoring cloud requirements cache because cached identity is incomplete."
	case cacheIdentityMismatch:
		return "Ignoring cloud requirements cache for different auth identity."
	case cacheExpired:
		return "Cloud requirements cache expired."
	default:
		return "Cloud requirements cache load failed."
	}
}

// formatParseFailedMessage mirrors the Rust
// `format_cloud_requirements_parse_failed_message`.
func formatParseFailedMessage(err error) string {
	return fmt.Sprintf("%s\n\nDetails:\n%s", parseFailedMessage, err)
}

// isEmptyRequirements reports whether the TOML contents represent "no
// requirements": empty/whitespace-only, or only comments/blank lines. It mirrors
// the Rust `parse_cloud_requirements` empty checks. The full requirements schema
// is not modeled in this port, so contents are kept as a raw string.
func isEmptyRequirements(contents string) bool {
	if strings.TrimSpace(contents) == "" {
		return true
	}
	for _, line := range strings.Split(contents, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		return false
	}
	return true
}
