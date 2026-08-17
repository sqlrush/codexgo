package write

import (
	"strings"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

const fixedPrefix = "2025-02-11T15-35-19-jqmb"

func fixedThreadID() protocol.ThreadID {
	return protocol.NewThreadID("0194f5a6-89ab-7cde-8123-456789abcdef")
}

func stage1OutputWithSlug(threadID protocol.ThreadID, slug *string) Stage1Output {
	return Stage1Output{
		ThreadID:        threadID,
		SourceUpdatedAt: time.Unix(123, 0).UTC(),
		RawMemory:       "raw memory",
		RolloutSummary:  "summary",
		RolloutSlug:     slug,
		RolloutPath:     "/tmp/rollout.jsonl",
		CWD:             "/tmp/workspace",
		GeneratedAt:     time.Unix(124, 0).UTC(),
	}
}

func TestRolloutSummaryFileStemUsesUUIDTimestampWhenSlugMissing(t *testing.T) {
	memory := stage1OutputWithSlug(fixedThreadID(), nil)
	if got := RolloutSummaryFileStem(&memory); got != fixedPrefix {
		t.Fatalf("stem = %q, want %q", got, fixedPrefix)
	}
}

func TestRolloutSummaryFileStemUsesUUIDTimestampWhenSlugEmpty(t *testing.T) {
	empty := ""
	memory := stage1OutputWithSlug(fixedThreadID(), &empty)
	if got := RolloutSummaryFileStem(&memory); got != fixedPrefix {
		t.Fatalf("stem = %q, want %q", got, fixedPrefix)
	}
}

func TestRolloutSummaryFileStemSanitizesAndTruncatesSlug(t *testing.T) {
	slug := "Unsafe Slug/With Spaces & Symbols + EXTRA_LONG_12345_67890_ABCDE_fghij_klmno"
	memory := stage1OutputWithSlug(fixedThreadID(), &slug)

	stem := RolloutSummaryFileStem(&memory)
	suffix, ok := strings.CutPrefix(stem, fixedPrefix+"-")
	if !ok {
		t.Fatalf("stem %q missing expected prefix", stem)
	}
	if len(suffix) != 60 {
		t.Fatalf("slug length = %d, want 60", len(suffix))
	}
	want := "unsafe_slug_with_spaces___symbols___extra_long_12345_67890_a"
	if suffix != want {
		t.Fatalf("slug = %q, want %q", suffix, want)
	}
}

func TestRolloutSummaryFileStemNonUUIDThreadID(t *testing.T) {
	// A non-UUID thread id falls back to the source timestamp + polynomial hash.
	memory := stage1OutputWithSlug(protocol.NewThreadID("legacy-thread"), nil)
	stem := RolloutSummaryFileStem(&memory)
	if !strings.HasPrefix(stem, "1970-01-01T00-02-03-") {
		t.Fatalf("stem = %q, want source-timestamp prefix", stem)
	}
	if len(stem) != len("1970-01-01T00-02-03-")+4 {
		t.Fatalf("stem = %q, want 4-char hash suffix", stem)
	}
}

func TestEncodeShortHash(t *testing.T) {
	tests := []struct {
		in   uint32
		want string
	}{
		{0, "0000"},
		{1, "0001"},
		{61, "000Z"},
		{62, "0010"},
	}
	for _, tc := range tests {
		if got := encodeShortHash(tc.in); got != tc.want {
			t.Errorf("encodeShortHash(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRFC3339MatchesChronoFormat(t *testing.T) {
	tests := []struct {
		name string
		in   time.Time
		want string
	}{
		{"whole seconds use plus offset", time.Unix(123, 0).UTC(), "1970-01-01T00:02:03+00:00"},
		{"milliseconds", time.Unix(0, 123_000_000).UTC(), "1970-01-01T00:00:00.123+00:00"},
		{"microseconds", time.Unix(0, 123_456_000).UTC(), "1970-01-01T00:00:00.123456+00:00"},
		{"nanoseconds", time.Unix(0, 123_456_789).UTC(), "1970-01-01T00:00:00.123456789+00:00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := rfc3339(tc.in); got != tc.want {
				t.Fatalf("rfc3339 = %q, want %q", got, tc.want)
			}
		})
	}
}
