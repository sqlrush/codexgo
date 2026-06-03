package unifiedexec

import "time"

// collectOutputUntilDeadline drains retained output into a single buffer,
// blocking on the output notify until the deadline or until the process exits
// and its output channel closes. Mirrors collect_output_until_deadline. The
// pause-state extension (driven by the core session) is omitted because that
// state lives outside this package's surface.
func collectOutputUntilDeadline(h outputHandles, deadline time.Time) []byte {
	collected := make([]byte, 0, 4096)
	exitSignalReceived := h.cancellation.isCancelled()
	var postExitDeadlineSet bool
	var postExitDeadline time.Time

	for {
		// Drain whatever is buffered, registering a waiter before checking so we
		// do not miss a notify that races with an empty drain.
		h.outputBufferMu.Lock()
		drained := h.outputBuffer.DrainChunks()
		var waitOutput <-chan struct{}
		if len(drained) == 0 {
			waitOutput = h.outputNotify.waiter()
		}
		h.outputBufferMu.Unlock()

		if len(drained) == 0 {
			exitSignalReceived = exitSignalReceived || h.cancellation.isCancelled()
			if exitSignalReceived && h.outputClosed.Load() {
				return collected
			}
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return collected
			}

			if exitSignalReceived {
				now := time.Now()
				if !postExitDeadlineSet {
					waitCap := remaining
					if waitCap > postExitCloseWaitCap {
						waitCap = postExitCloseWaitCap
					}
					postExitDeadline = now.Add(waitCap)
					postExitDeadlineSet = true
				}
				closeWaitRemaining := time.Until(postExitDeadline)
				if closeWaitRemaining <= 0 {
					return collected
				}
				closedWaiter := h.outputClosedNotify.waiter()
				select {
				case <-waitOutput:
				case <-closedWaiter:
				case <-time.After(closeWaitRemaining):
					return collected
				}
				continue
			}

			select {
			case <-waitOutput:
			case <-h.cancellation.cancelled():
				exitSignalReceived = true
			case <-time.After(remaining):
				return collected
			}
			continue
		}

		for _, chunk := range drained {
			collected = append(collected, chunk...)
		}

		exitSignalReceived = exitSignalReceived || h.cancellation.isCancelled()
		if !time.Now().Before(deadline) {
			return collected
		}
	}
}
