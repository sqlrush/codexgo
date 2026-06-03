package feedback

// ringBuffer is a byte ring buffer that retains at most max trailing bytes.
// Mirrors Rust `RingBuffer`.
type ringBuffer struct {
	max int
	buf []byte
}

func newRingBuffer(capacity int) *ringBuffer {
	return &ringBuffer{max: capacity, buf: make([]byte, 0, capacity)}
}

// pushBytes appends data, evicting from the front to honor the capacity.
// Mirrors Rust `RingBuffer::push_bytes`.
func (r *ringBuffer) pushBytes(data []byte) {
	if len(data) == 0 {
		return
	}

	// If the incoming chunk is larger than capacity, keep only the trailing
	// bytes.
	if len(data) >= r.max {
		start := len(data) - r.max
		r.buf = append(r.buf[:0], data[start:]...)
		return
	}

	// Evict from the front if we would exceed capacity. Compact in place so the
	// backing array does not grow unbounded as the front is dropped.
	needed := len(r.buf) + len(data)
	if needed > r.max {
		toDrop := needed - r.max
		n := copy(r.buf, r.buf[toDrop:])
		r.buf = r.buf[:n]
	}

	r.buf = append(r.buf, data...)
}

// snapshotBytes returns a copy of the current contents. Mirrors Rust
// `RingBuffer::snapshot_bytes`.
func (r *ringBuffer) snapshotBytes() []byte {
	out := make([]byte, len(r.buf))
	copy(out, r.buf)
	return out
}
