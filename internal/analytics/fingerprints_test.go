package analytics

import (
	"reflect"
	"testing"
)

func TestAcceptedLineFingerprintsFromUnifiedDiff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		diff string
		want AcceptedLineFingerprintSummary
	}{
		{
			name: "parses_counts_and_effective_added_fingerprints",
			diff: "diff --git a/src/lib.rs b/src/lib.rs\n" +
				"index 1111111..2222222\n" +
				"--- a/src/lib.rs\n" +
				"+++ b/src/lib.rs\n" +
				"@@ -1,3 +1,5 @@\n" +
				"-old line\n" +
				"+fn useful() {\n" +
				"+}\n" +
				"+    return user.id;\n" +
				" context\n",
			want: AcceptedLineFingerprintSummary{
				AcceptedAddedLines:   3,
				AcceptedDeletedLines: 1,
				LineFingerprints: []AcceptedLineFingerprint{
					{
						PathHash: FingerprintHash("path", "src/lib.rs"),
						LineHash: FingerprintHash("line", "fn useful() {"),
					},
					{
						PathHash: FingerprintHash("path", "src/lib.rs"),
						LineHash: FingerprintHash("line", "return user.id;"),
					},
				},
			},
		},
		{
			name: "skips_added_file_metadata_headers",
			diff: "diff --git a/new.py b/new.py\n" +
				"new file mode 100644\n" +
				"index 0000000..1111111\n" +
				"--- /dev/null\n" +
				"+++ b/new.py\n" +
				"@@ -0,0 +1 @@\n" +
				"+print('hello')\n",
			want: AcceptedLineFingerprintSummary{
				AcceptedAddedLines:   1,
				AcceptedDeletedLines: 0,
				LineFingerprints: []AcceptedLineFingerprint{
					{
						PathHash: FingerprintHash("path", "new.py"),
						LineHash: FingerprintHash("line", "print('hello')"),
					},
				},
			},
		},
		{
			name: "parses_hunk_lines_that_look_like_file_headers",
			diff: "diff --git a/src/lib.rs b/src/lib.rs\n" +
				"index 1111111..2222222\n" +
				"--- a/src/lib.rs\n" +
				"+++ b/src/lib.rs\n" +
				"@@ -1,2 +1,2 @@\n" +
				"--- old value\n" +
				"+++ new value\n",
			want: AcceptedLineFingerprintSummary{
				AcceptedAddedLines:   1,
				AcceptedDeletedLines: 1,
				LineFingerprints: []AcceptedLineFingerprint{
					{
						PathHash: FingerprintHash("path", "src/lib.rs"),
						LineHash: FingerprintHash("line", "++ new value"),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := AcceptedLineFingerprintsFromUnifiedDiff(tt.diff)
			if got.AcceptedAddedLines != tt.want.AcceptedAddedLines {
				t.Errorf("added: got %d want %d", got.AcceptedAddedLines, tt.want.AcceptedAddedLines)
			}
			if got.AcceptedDeletedLines != tt.want.AcceptedDeletedLines {
				t.Errorf("deleted: got %d want %d", got.AcceptedDeletedLines, tt.want.AcceptedDeletedLines)
			}
			if !reflect.DeepEqual(got.LineFingerprints, tt.want.LineFingerprints) {
				t.Errorf("fingerprints:\n got %#v\nwant %#v", got.LineFingerprints, tt.want.LineFingerprints)
			}
		})
	}
}

func TestFingerprintHashStability(t *testing.T) {
	t.Parallel()
	// The hash is a domain-separated SHA-1 over "file-line-v1\0" + domain +
	// "\0" + value. Verify determinism and 40-hex-char output.
	got := FingerprintHash("path", "src/lib.rs")
	if len(got) != 40 {
		t.Fatalf("expected 40 hex chars, got %d (%q)", len(got), got)
	}
	if got != FingerprintHash("path", "src/lib.rs") {
		t.Fatal("hash is not deterministic")
	}
	// Domain separation: different domain yields a different hash.
	if got == FingerprintHash("line", "src/lib.rs") {
		t.Fatal("expected domain separation")
	}
}
