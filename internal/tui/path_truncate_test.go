package tui

import "testing"

// These cases are ported verbatim from codex-rs/tui/src/text_formatting.rs tests
// for center_truncate_path, proving the Go port produces byte-identical results.
func TestCenterTruncatePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		maxWidth int
		want     string
	}{
		{
			name:     "short path is unchanged",
			path:     "/Users/codex/Public",
			maxWidth: 40,
			want:     "/Users/codex/Public",
		},
		{
			name:     "long path keeps head and tail with ellipsis",
			path:     "~/hello/the/fox/is/very/fast",
			maxWidth: 24,
			want:     "~/hello/the/…/very/fast",
		},
		{
			name:     "long windows-style path",
			path:     "C:/Users/codex/Projects/super/long/windows/path/file.txt",
			maxWidth: 36,
			want:     "C:/Users/codex/…/path/file.txt",
		},
		{
			name:     "front-truncates a long single segment",
			path:     "~/supercalifragilisticexpialidocious",
			maxWidth: 18,
			want:     "~/…cexpialidocious",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := centerTruncatePath(tt.path, tt.maxWidth)
			if got != tt.want {
				t.Errorf("centerTruncatePath(%q, %d) = %q, want %q", tt.path, tt.maxWidth, got, tt.want)
			}
		})
	}
}
