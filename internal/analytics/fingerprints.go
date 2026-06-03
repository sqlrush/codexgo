// Package analytics is a faithful port of codex-rs/analytics. It provides the
// privacy-respecting analytics event client and the fact/event types that are
// uploaded to the Codex analytics backend.
//
// Analytics is DISABLED by default. A non-nil queue is only created when the
// caller opts in via the [analytics] config section (analytics_enabled != Some(false)).
package analytics

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
)

// AcceptedLineFingerprint is a privacy-preserving fingerprint of an accepted
// added line. Mirrors Rust `AcceptedLineFingerprint`.
type AcceptedLineFingerprint struct {
	PathHash string `json:"path_hash"`
	LineHash string `json:"line_hash"`
}

// AcceptedLineFingerprintSummary aggregates the counts and fingerprints parsed
// from a unified diff. Mirrors Rust `AcceptedLineFingerprintSummary`.
type AcceptedLineFingerprintSummary struct {
	AcceptedAddedLines   uint64
	AcceptedDeletedLines uint64
	LineFingerprints     []AcceptedLineFingerprint
}

// AcceptedLineFingerprintsFromUnifiedDiff parses a unified diff and returns the
// accepted line counts plus privacy-preserving fingerprints for each effective
// added line. Mirrors Rust `accepted_line_fingerprints_from_unified_diff`.
func AcceptedLineFingerprintsFromUnifiedDiff(unifiedDiff string) AcceptedLineFingerprintSummary {
	var currentPath string
	hasCurrentPath := false
	inHunk := false
	var acceptedAdded uint64
	var acceptedDeleted uint64
	lineFingerprints := make([]AcceptedLineFingerprint, 0)

	for _, line := range splitLines(unifiedDiff) {
		if strings.HasPrefix(line, "diff --git ") {
			currentPath = ""
			hasCurrentPath = false
			inHunk = false
			continue
		}

		if strings.HasPrefix(line, "@@ ") {
			inHunk = true
			continue
		}

		if !inHunk {
			if path, ok := strings.CutPrefix(line, "+++ "); ok {
				if normalized, ok := normalizeDiffPath(path); ok {
					currentPath = normalized
					hasCurrentPath = true
				} else {
					currentPath = ""
					hasCurrentPath = false
				}
				continue
			}
			if strings.HasPrefix(line, "--- ") {
				continue
			}
		}

		if addedLine, ok := strings.CutPrefix(line, "+"); ok {
			acceptedAdded++
			if hasCurrentPath {
				if normalizedLine, ok := normalizeEffectiveLine(addedLine); ok {
					lineFingerprints = append(lineFingerprints, AcceptedLineFingerprint{
						PathHash: FingerprintHash("path", currentPath),
						LineHash: FingerprintHash("line", normalizedLine),
					})
				}
			}
			continue
		}

		if strings.HasPrefix(line, "-") {
			acceptedDeleted++
		}
	}

	return AcceptedLineFingerprintSummary{
		AcceptedAddedLines:   acceptedAdded,
		AcceptedDeletedLines: acceptedDeleted,
		LineFingerprints:     lineFingerprints,
	}
}

// FingerprintHash produces a domain-separated SHA-1 fingerprint matching the
// Rust `fingerprint_hash`. The byte layout is exactly:
// "file-line-v1\0" + domain + "\0" + value, hex-encoded lowercase.
func FingerprintHash(domain, value string) string {
	h := sha1.New()
	h.Write([]byte("file-line-v1\x00"))
	h.Write([]byte(domain))
	h.Write([]byte{0})
	h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil))
}

// splitLines mirrors Rust's str::lines: it splits on '\n', strips a trailing
// '\r' from each line, and does not yield a trailing empty line.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\n")
	// Drop a trailing empty element produced by a final newline.
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	for i, p := range parts {
		parts[i] = strings.TrimSuffix(p, "\r")
	}
	return parts
}

// normalizeDiffPath strips the a/ or b/ prefix and rejects /dev/null. Mirrors
// Rust `normalize_diff_path`.
func normalizeDiffPath(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "/dev/null" {
		return "", false
	}
	if stripped, ok := strings.CutPrefix(path, "b/"); ok {
		return stripped, true
	}
	if stripped, ok := strings.CutPrefix(path, "a/"); ok {
		return stripped, true
	}
	return path, true
}

// normalizeEffectiveLine collapses whitespace and rejects trivial lines.
// Mirrors Rust `normalize_effective_line`.
func normalizeEffectiveLine(line string) (string, bool) {
	normalized := strings.Join(strings.Fields(line), " ")
	if len(normalized) <= 3 {
		return "", false
	}
	hasAlnum := false
	for _, ch := range normalized {
		if isAlphanumeric(ch) || ch == '_' {
			hasAlnum = true
			break
		}
	}
	if !hasAlnum {
		return "", false
	}
	return normalized, true
}

func isAlphanumeric(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		// Rust char::is_alphanumeric is Unicode-aware; approximate for the
		// common non-ASCII alphanumeric case used by diff content.
		(ch > 127 && (isUnicodeLetter(ch) || isUnicodeDigit(ch)))
}

func isUnicodeLetter(ch rune) bool {
	return ch >= 0xC0 && ch != 0xD7 && ch != 0xF7
}

func isUnicodeDigit(ch rune) bool {
	return false
}
