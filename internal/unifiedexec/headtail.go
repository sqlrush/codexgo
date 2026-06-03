package unifiedexec

// HeadTailBuffer is a capped buffer that preserves a stable prefix ("head") and
// suffix ("tail"), dropping the middle once it exceeds the configured maximum.
// The buffer is symmetric: half the capacity is allocated to the head and half
// to the tail. Mirrors HeadTailBuffer in head_tail_buffer.rs.
//
// It is not safe for concurrent use; callers guard it with their own lock.
type HeadTailBuffer struct {
	maxBytes    int
	headBudget  int
	tailBudget  int
	head        [][]byte
	tail        [][]byte
	headBytes   int
	tailBytes   int
	omittedSize int
}

// NewHeadTailBuffer creates a buffer that retains at most maxBytes of output,
// split across a head and tail budget. Mirrors HeadTailBuffer::new.
func NewHeadTailBuffer(maxBytes int) *HeadTailBuffer {
	headBudget := maxBytes / 2
	tailBudget := maxBytes - headBudget
	if tailBudget < 0 {
		tailBudget = 0
	}
	return &HeadTailBuffer{
		maxBytes:   maxBytes,
		headBudget: headBudget,
		tailBudget: tailBudget,
	}
}

// NewDefaultHeadTailBuffer returns a buffer capped at UnifiedExecOutputMaxBytes.
// Mirrors HeadTailBuffer::default.
func NewDefaultHeadTailBuffer() *HeadTailBuffer {
	return NewHeadTailBuffer(UnifiedExecOutputMaxBytes)
}

// RetainedBytes reports the total bytes currently retained (head + tail).
// Mirrors retained_bytes.
func (b *HeadTailBuffer) RetainedBytes() int {
	return b.headBytes + b.tailBytes
}

// OmittedBytes reports the total bytes dropped from the middle due to the cap.
// Mirrors omitted_bytes.
func (b *HeadTailBuffer) OmittedBytes() int {
	return b.omittedSize
}

// PushChunk appends a chunk, filling the head budget first, then keeping a
// capped tail. Mirrors push_chunk. The chunk is consumed (not retained by the
// caller after the call); copies are taken where parts must be kept.
func (b *HeadTailBuffer) PushChunk(chunk []byte) {
	if b.maxBytes == 0 {
		b.omittedSize += len(chunk)
		return
	}

	if b.headBytes < b.headBudget {
		remainingHead := b.headBudget - b.headBytes
		if len(chunk) <= remainingHead {
			b.headBytes += len(chunk)
			b.head = append(b.head, cloneBytes(chunk))
			return
		}

		headPart := chunk[:remainingHead]
		tailPart := chunk[remainingHead:]
		if len(headPart) > 0 {
			b.headBytes += len(headPart)
			b.head = append(b.head, cloneBytes(headPart))
		}
		b.pushToTail(cloneBytes(tailPart))
		return
	}

	b.pushToTail(cloneBytes(chunk))
}

// SnapshotChunks returns a copy of the retained output as a list of chunks,
// head chunks first then tail chunks. Mirrors snapshot_chunks.
func (b *HeadTailBuffer) SnapshotChunks() [][]byte {
	out := make([][]byte, 0, len(b.head)+len(b.tail))
	for _, c := range b.head {
		out = append(out, cloneBytes(c))
	}
	for _, c := range b.tail {
		out = append(out, cloneBytes(c))
	}
	return out
}

// ToBytes returns the retained output as a single byte slice (head then tail).
// Mirrors to_bytes.
func (b *HeadTailBuffer) ToBytes() []byte {
	out := make([]byte, 0, b.RetainedBytes())
	for _, c := range b.head {
		out = append(out, c...)
	}
	for _, c := range b.tail {
		out = append(out, c...)
	}
	return out
}

// DrainChunks removes and returns all retained chunks (head then tail), resetting
// the buffer state. Mirrors drain_chunks.
func (b *HeadTailBuffer) DrainChunks() [][]byte {
	out := make([][]byte, 0, len(b.head)+len(b.tail))
	out = append(out, b.head...)
	out = append(out, b.tail...)
	b.head = nil
	b.tail = nil
	b.headBytes = 0
	b.tailBytes = 0
	b.omittedSize = 0
	return out
}

// pushToTail appends chunk to the tail, trimming the oldest tail bytes to stay
// within the tail budget. Mirrors push_to_tail.
func (b *HeadTailBuffer) pushToTail(chunk []byte) {
	if b.tailBudget == 0 {
		b.omittedSize += len(chunk)
		return
	}

	if len(chunk) >= b.tailBudget {
		// This single chunk is larger than the whole tail budget. Keep only the
		// last tail_budget bytes and drop everything else.
		start := len(chunk) - b.tailBudget
		kept := cloneBytes(chunk[start:])
		dropped := len(chunk) - len(kept)
		b.omittedSize += b.tailBytes + dropped
		b.tail = nil
		b.tailBytes = len(kept)
		b.tail = append(b.tail, kept)
		return
	}

	b.tailBytes += len(chunk)
	b.tail = append(b.tail, chunk)
	b.trimTailToBudget()
}

// trimTailToBudget drops the oldest tail bytes until the tail fits its budget.
// Mirrors trim_tail_to_budget.
func (b *HeadTailBuffer) trimTailToBudget() {
	excess := b.tailBytes - b.tailBudget
	for excess > 0 {
		if len(b.tail) == 0 {
			break
		}
		front := b.tail[0]
		if excess >= len(front) {
			excess -= len(front)
			b.tailBytes -= len(front)
			b.omittedSize += len(front)
			b.tail = b.tail[1:]
			continue
		}
		// Drop the first `excess` bytes of the front chunk.
		b.tail[0] = front[excess:]
		b.tailBytes -= excess
		b.omittedSize += excess
		break
	}
}

// cloneBytes returns a fresh copy of b so retained chunks never alias caller
// buffers (the readers in internal/pty reuse a single read buffer).
func cloneBytes(b []byte) []byte {
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
