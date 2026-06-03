package unifiedexec

import (
	"strings"
	"testing"
)

func TestHeadTailBuffer(t *testing.T) {
	t.Run("keeps prefix and suffix when over budget", func(t *testing.T) {
		buf := NewHeadTailBuffer(10)
		buf.PushChunk([]byte("0123456789"))
		if buf.OmittedBytes() != 0 {
			t.Fatalf("omitted = %d, want 0", buf.OmittedBytes())
		}
		buf.PushChunk([]byte("ab"))
		if buf.OmittedBytes() == 0 {
			t.Fatalf("omitted = 0, want > 0")
		}
		rendered := string(buf.ToBytes())
		if !strings.HasPrefix(rendered, "01234") {
			t.Fatalf("rendered = %q, want prefix 01234", rendered)
		}
		if !strings.HasSuffix(rendered, "89ab") {
			t.Fatalf("rendered = %q, want suffix 89ab", rendered)
		}
	})

	t.Run("max bytes zero drops everything", func(t *testing.T) {
		buf := NewHeadTailBuffer(0)
		buf.PushChunk([]byte("abc"))
		if buf.RetainedBytes() != 0 {
			t.Fatalf("retained = %d, want 0", buf.RetainedBytes())
		}
		if buf.OmittedBytes() != 3 {
			t.Fatalf("omitted = %d, want 3", buf.OmittedBytes())
		}
		if got := buf.ToBytes(); len(got) != 0 {
			t.Fatalf("to_bytes = %q, want empty", got)
		}
		if got := buf.SnapshotChunks(); len(got) != 0 {
			t.Fatalf("snapshot = %v, want empty", got)
		}
	})

	t.Run("head budget zero keeps only last byte in tail", func(t *testing.T) {
		buf := NewHeadTailBuffer(1)
		buf.PushChunk([]byte("abc"))
		if buf.RetainedBytes() != 1 {
			t.Fatalf("retained = %d, want 1", buf.RetainedBytes())
		}
		if buf.OmittedBytes() != 2 {
			t.Fatalf("omitted = %d, want 2", buf.OmittedBytes())
		}
		if got := string(buf.ToBytes()); got != "c" {
			t.Fatalf("to_bytes = %q, want c", got)
		}
	})

	t.Run("draining resets state", func(t *testing.T) {
		buf := NewHeadTailBuffer(10)
		buf.PushChunk([]byte("0123456789"))
		buf.PushChunk([]byte("ab"))
		drained := buf.DrainChunks()
		if len(drained) == 0 {
			t.Fatalf("drained empty, want non-empty")
		}
		if buf.RetainedBytes() != 0 || buf.OmittedBytes() != 0 || len(buf.ToBytes()) != 0 {
			t.Fatalf("buffer not reset after drain")
		}
	})

	t.Run("chunk larger than tail budget keeps only tail end", func(t *testing.T) {
		buf := NewHeadTailBuffer(10)
		buf.PushChunk([]byte("0123456789"))
		buf.PushChunk([]byte("ABCDEFGHIJK"))
		out := string(buf.ToBytes())
		if !strings.HasPrefix(out, "01234") {
			t.Fatalf("out = %q, want prefix 01234", out)
		}
		if !strings.HasSuffix(out, "GHIJK") {
			t.Fatalf("out = %q, want suffix GHIJK", out)
		}
		if buf.OmittedBytes() == 0 {
			t.Fatalf("omitted = 0, want > 0")
		}
	})

	t.Run("fills head then tail across multiple chunks", func(t *testing.T) {
		buf := NewHeadTailBuffer(10)
		buf.PushChunk([]byte("01"))
		buf.PushChunk([]byte("234"))
		if got := string(buf.ToBytes()); got != "01234" {
			t.Fatalf("to_bytes = %q, want 01234", got)
		}
		buf.PushChunk([]byte("567"))
		buf.PushChunk([]byte("89"))
		if got := string(buf.ToBytes()); got != "0123456789" {
			t.Fatalf("to_bytes = %q, want 0123456789", got)
		}
		if buf.OmittedBytes() != 0 {
			t.Fatalf("omitted = %d, want 0", buf.OmittedBytes())
		}
		buf.PushChunk([]byte("a"))
		if got := string(buf.ToBytes()); got != "012346789a" {
			t.Fatalf("to_bytes = %q, want 012346789a", got)
		}
		if buf.OmittedBytes() != 1 {
			t.Fatalf("omitted = %d, want 1", buf.OmittedBytes())
		}
	})
}
