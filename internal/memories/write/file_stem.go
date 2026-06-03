package write

import (
	"fmt"
	"strings"
	"time"
)

const (
	rolloutSlugMaxLen = 60
	// shortHashSpace is 62^4, the size of the 4-character base-62 hash space.
	shortHashSpace  = 14_776_336
	timestampLayout = "2006-01-02T15-04-05"
)

// shortHashAlphabet is the base-62 alphabet used for the 4-character short hash,
// mirroring SHORT_HASH_ALPHABET (digits, then lowercase, then uppercase).
const shortHashAlphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// RolloutSummaryFileStem returns the canonical rollout-summary file stem for a
// stage-1 output, mirroring rollout_summary_file_stem.
func RolloutSummaryFileStem(memory *Stage1Output) string {
	return rolloutSummaryFileStemFromParts(
		memory.ThreadID.String(),
		memory.SourceUpdatedAt,
		memory.RolloutSlug,
	)
}

// rolloutSummaryFileStemFromParts builds the file stem from primitive parts,
// mirroring rollout_summary_file_stem_from_parts. When the thread id is a valid
// UUID, its embedded timestamp (v1/v6/v7) drives the timestamp fragment and its
// low 32 bits seed the short hash; otherwise the source timestamp and a polynomial
// hash of the id bytes are used.
func rolloutSummaryFileStemFromParts(threadID string, sourceUpdatedAt time.Time, rolloutSlug *string) string {
	var timestampFragment string
	var shortHashSeed uint32

	if uuidVal, ok := parseUUID(threadID); ok {
		timestamp := sourceUpdatedAt.UTC()
		if ts, ok := uuidTimestamp(uuidVal); ok {
			timestamp = ts
		}
		timestampFragment = timestamp.Format(timestampLayout)
		shortHashSeed = uint32(uuidVal.asU128Low32())
	} else {
		var seed uint32
		for i := 0; i < len(threadID); i++ {
			seed = seed*31 + uint32(threadID[i])
		}
		shortHashSeed = seed
		timestampFragment = sourceUpdatedAt.UTC().Format(timestampLayout)
	}

	shortHash := encodeShortHash(shortHashSeed % shortHashSpace)
	filePrefix := fmt.Sprintf("%s-%s", timestampFragment, shortHash)

	if rolloutSlug == nil {
		return filePrefix
	}

	slug := sanitizeSlug(*rolloutSlug)
	if slug == "" {
		return filePrefix
	}
	return fmt.Sprintf("%s-%s", filePrefix, slug)
}

// encodeShortHash renders value as a fixed-width 4-character base-62 string,
// mirroring the most-significant-digit-first encoding in the Rust source.
func encodeShortHash(value uint32) string {
	chars := []byte{'0', '0', '0', '0'}
	base := uint32(len(shortHashAlphabet))
	for idx := len(chars) - 1; idx >= 0; idx-- {
		chars[idx] = shortHashAlphabet[value%base]
		value /= base
	}
	return string(chars)
}

// sanitizeSlug lowercases ASCII alphanumerics, replaces every other character
// with an underscore, truncates to 60 characters, and strips trailing
// underscores, mirroring the slug-building loop in the Rust source.
func sanitizeSlug(raw string) string {
	var b strings.Builder
	for _, ch := range raw {
		if b.Len() >= rolloutSlugMaxLen {
			break
		}
		if isASCIIAlphanumeric(ch) {
			b.WriteRune(toASCIILower(ch))
		} else {
			b.WriteByte('_')
		}
	}
	return strings.TrimRight(b.String(), "_")
}

func isASCIIAlphanumeric(ch rune) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func toASCIILower(ch rune) rune {
	if ch >= 'A' && ch <= 'Z' {
		return ch + ('a' - 'A')
	}
	return ch
}
