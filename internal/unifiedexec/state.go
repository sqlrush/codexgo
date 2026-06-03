package unifiedexec

// processState is the shared exit/failure state tracked for a unified-exec
// process. It is an immutable value: the with-style helpers return a new copy
// rather than mutating the receiver. Mirrors ProcessState in process_state.rs.
type processState struct {
	hasExited      bool
	exitCode       *int
	failureMessage *string
}

// exited returns a copy marked as exited with the given exit code, preserving
// any existing failure message. Mirrors ProcessState::exited.
func (s processState) exited(exitCode *int) processState {
	return processState{
		hasExited:      true,
		exitCode:       exitCode,
		failureMessage: s.failureMessage,
	}
}

// failed returns a copy marked as exited with the given failure message,
// preserving the existing exit code. Mirrors ProcessState::failed.
func (s processState) failed(message string) processState {
	msg := message
	return processState{
		hasExited:      true,
		exitCode:       s.exitCode,
		failureMessage: &msg,
	}
}
