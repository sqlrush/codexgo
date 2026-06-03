package pty

import (
	"errors"
	"io"
	"sync"
)

// errNotPTY is returned by Resize when the process has no attached terminal.
var errNotPTY = errors.New("pty: process is not attached to a PTY")

// SpawnedProcess is the return value of the spawn helpers, bundling the process
// handle with the split output channels and the exit-code channel. Mirrors the
// Rust SpawnedProcess struct.
//
// Stdout and Stderr deliver byte chunks as they are read; each is closed when
// its stream reaches EOF. Exit delivers exactly one exit code and is then
// closed. For PTY spawns stderr is folded into stdout (the child sees a single
// terminal), so Stderr is closed immediately with no data, matching codex.
type SpawnedProcess struct {
	Process *Process
	Stdout  <-chan []byte
	Stderr  <-chan []byte
	Exit    <-chan int
}

// CombineOutput merges split stdout and stderr channels into a single channel
// that is closed once both inputs are drained. Mirrors combine_output_receivers.
// Order between the two streams is not guaranteed, matching the Rust select-based
// implementation.
func CombineOutput(stdout, stderr <-chan []byte) <-chan []byte {
	out := make(chan []byte, 256)
	go func() {
		defer close(out)
		for stdout != nil || stderr != nil {
			select {
			case chunk, ok := <-stdout:
				if !ok {
					stdout = nil
					continue
				}
				out <- chunk
			case chunk, ok := <-stderr:
				if !ok {
					stderr = nil
					continue
				}
				out <- chunk
			}
		}
	}()
	return out
}

// readStream reads chunks from r and forwards copies on out until EOF or error,
// then closes out. The wg is decremented when the goroutine returns. Each chunk
// is a fresh slice so callers may retain it safely.
func readStream(r io.Reader, out chan<- []byte, wg *sync.WaitGroup) {
	defer wg.Done()
	defer close(out)
	buf := make([]byte, readBufferSize)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			out <- chunk
		}
		if err != nil {
			return
		}
	}
}
