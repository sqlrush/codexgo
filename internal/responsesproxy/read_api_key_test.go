package responsesproxy

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// chunkReader returns the provided chunks one per Read call, then io.EOF. A
// chunk longer than the destination buffer is truncated to the buffer length.
type chunkReader struct {
	chunks [][]byte
	idx    int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.idx >= len(r.chunks) {
		return 0, io.EOF
	}
	chunk := r.chunks[r.idx]
	r.idx++
	n := copy(p, chunk)
	return n, nil
}

func TestReadAuthHeaderFromStdin(t *testing.T) {
	tests := []struct {
		name    string
		chunks  [][]byte
		want    string
		wantErr string
	}{
		{
			name:   "no newlines",
			chunks: [][]byte{[]byte("sk-abc123")},
			want:   "Bearer sk-abc123",
		},
		{
			name:   "short reads",
			chunks: [][]byte{[]byte("sk-"), []byte("abc"), []byte("123\n")},
			want:   "Bearer sk-abc123",
		},
		{
			name:   "trims crlf",
			chunks: [][]byte{[]byte("sk-abc123\r\n")},
			want:   "Bearer sk-abc123",
		},
		{
			name:    "no input",
			chunks:  nil,
			wantErr: "must be provided",
		},
		{
			name:    "invalid utf8 char",
			chunks:  [][]byte{{'s', 'k', '-', 'a', 'b', 'c', 0xff}},
			wantErr: "API key may only contain ASCII letters, numbers, '-' or '_'",
		},
		{
			name:    "invalid character",
			chunks:  [][]byte{[]byte("sk-abc!23")},
			wantErr: "API key may only contain ASCII letters, numbers, '-' or '_'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &chunkReader{chunks: tt.chunks}
			got, err := ReadAuthHeaderFromStdin(r)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("got err %v want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestReadAuthHeaderBufferFilled(t *testing.T) {
	data := strings.Repeat("a", bufferSize-len(authHeaderPrefix))
	r := &chunkReader{chunks: [][]byte{[]byte(data)}}
	_, err := ReadAuthHeaderFromStdin(r)
	if err == nil {
		t.Fatalf("expected buffer-filled error")
	}
	wantMsg := "API key is too large to fit in the 1024-byte buffer"
	if !strings.Contains(err.Error(), wantMsg) {
		t.Fatalf("got %v want containing %q", err, wantMsg)
	}
}

func TestReadAuthHeaderPropagatesIOError(t *testing.T) {
	boom := errors.New("boom")
	_, err := readAuthHeaderWith(func([]byte) (int, error) {
		return 0, boom
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("got %v want containing boom", err)
	}
}

func TestValidateAuthHeaderBytes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{name: "alnum", in: "sk-abc_123-XYZ", ok: true},
		{name: "bang", in: "sk!", ok: false},
		{name: "space", in: "sk abc", ok: false},
		{name: "nul", in: "sk\x00", ok: false},
		{name: "empty", in: "", ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAuthHeaderBytes([]byte(tt.in))
			if (err == nil) != tt.ok {
				t.Fatalf("validateAuthHeaderBytes(%q) err=%v want ok=%v", tt.in, err, tt.ok)
			}
		})
	}
}
