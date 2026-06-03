package execserver

import (
	"context"
	"time"
)

// wakeReaders broadcasts to all readers blocked in execRead by closing the
// current wake channel and installing a fresh one. The caller must hold b.mu.
//
// Rust: `output_notify.notify_waiters()`.
func (rp *runningProcess) wakeReaders() {
	close(rp.wake)
	rp.wake = make(chan struct{})
}

// streamOutput forwards output chunks from a spawn channel into the retained
// buffer, evicting old chunks past the byte cap, and emits a process/output
// notification plus a pushed Output event for each chunk. When the channel
// closes it decrements the open-stream count and may emit a close.
//
// Rust: `stream_output` + `finish_output_stream`.
func (b *LocalProcess) streamOutput(id ProcessId, stream ExecOutputStream, receiver <-chan []byte) {
	for chunk := range receiver {
		notification, ok := b.recordOutput(id, stream, chunk)
		if !ok {
			break
		}
		_ = b.notificationSender().Notify(context.Background(), ExecOutputDeltaMethod, notification)
	}
	b.finishOutputStream(id)
}

// recordOutput appends a chunk to the retained buffer and returns the
// notification to send. It returns ok=false when the process is gone.
func (b *LocalProcess) recordOutput(id ProcessId, stream ExecOutputStream, chunk []byte) (ExecOutputDeltaNotification, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	entry, ok := b.processes[id.String()]
	if !ok || entry.running == nil {
		return ExecOutputDeltaNotification{}, false
	}
	rp := entry.running

	seq := rp.nextSeq
	rp.nextSeq++
	stored := make([]byte, len(chunk))
	copy(stored, chunk)
	rp.retainedBytes += len(stored)
	rp.output = append(rp.output, retainedOutputChunk{seq: seq, stream: stream, chunk: stored})
	for rp.retainedBytes > retainedOutputBytesPerProcess {
		if len(rp.output) == 0 {
			break
		}
		evicted := rp.output[0]
		rp.output = rp.output[1:]
		rp.retainedBytes -= len(evicted.chunk)
		if rp.retainedBytes < 0 {
			rp.retainedBytes = 0
		}
	}

	outChunk := ProcessOutputChunk{Seq: seq, Stream: stream, Chunk: append([]byte(nil), stored...)}
	rp.events.publish(NewOutputEvent(outChunk))
	rp.wakeReaders()

	return ExecOutputDeltaNotification{
		ProcessID: id,
		Seq:       seq,
		Stream:    stream,
		Chunk:     append([]byte(nil), stored...),
	}, true
}

// watchExit waits for the process exit code, records it, emits a process/exited
// notification, and may emit a close. Mirrors `watch_exit`.
func (b *LocalProcess) watchExit(id ProcessId, exitCh <-chan int) {
	exitCode, ok := <-exitCh
	if !ok {
		exitCode = -1
	}

	notification, send := b.recordExit(id, exitCode)
	if send {
		_ = b.notificationSender().Notify(context.Background(), ExecExitedMethod, notification)
	}

	b.maybeEmitClosed(id)
}

// recordExit stores the exit code and returns the notification to send.
func (b *LocalProcess) recordExit(id ProcessId, exitCode int) (ExecExitedNotification, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	entry, ok := b.processes[id.String()]
	if !ok || entry.running == nil {
		return ExecExitedNotification{}, false
	}
	rp := entry.running

	seq := rp.nextSeq
	rp.nextSeq++
	code := exitCode
	rp.exitCode = &code
	rp.events.publish(NewExitedEvent(seq, exitCode))
	rp.wakeReaders()

	return ExecExitedNotification{ProcessID: id, Seq: seq, ExitCode: exitCode}, true
}

// finishOutputStream decrements the open-stream count and may emit a close.
// Mirrors `finish_output_stream`.
func (b *LocalProcess) finishOutputStream(id ProcessId) {
	b.mu.Lock()
	entry, ok := b.processes[id.String()]
	if !ok || entry.running == nil {
		b.mu.Unlock()
		return
	}
	if entry.running.openStreams > 0 {
		entry.running.openStreams--
	}
	b.mu.Unlock()

	b.maybeEmitClosed(id)
}

// maybeEmitClosed emits a process/closed notification once all output streams
// have ended and the process has exited, then schedules eviction. Mirrors
// `maybe_emit_closed`.
func (b *LocalProcess) maybeEmitClosed(id ProcessId) {
	b.mu.Lock()
	entry, ok := b.processes[id.String()]
	if !ok || entry.running == nil {
		b.mu.Unlock()
		return
	}
	rp := entry.running
	if rp.closed || rp.openStreams != 0 || rp.exitCode == nil {
		b.mu.Unlock()
		return
	}

	rp.closed = true
	seq := rp.nextSeq
	rp.nextSeq++
	rp.events.publish(NewClosedEvent(seq))
	rp.wakeReaders()
	notification := ExecClosedNotification{ProcessID: id, Seq: seq}
	b.mu.Unlock()

	go b.scheduleEviction(id)

	_ = b.notificationSender().Notify(context.Background(), ExecClosedMethod, notification)
}

// scheduleEviction removes a closed process from the registry after the
// retention window. Mirrors the cleanup task spawned by `maybe_emit_closed`.
func (b *LocalProcess) scheduleEviction(id ProcessId) {
	time.Sleep(exitedProcessRetention)
	b.mu.Lock()
	defer b.mu.Unlock()
	entry, ok := b.processes[id.String()]
	if ok && entry.running != nil && entry.running.closed {
		delete(b.processes, id.String())
	}
}
