//go:build !unix

package pty

import (
	"os"
	"syscall"
)

// killProcessGroup is a best-effort no-op fallback used by the process handle on
// platforms without Unix process groups; pipe spawn kills the child directly.
func killProcessGroup(int) error { return nil }

// pidTerminator kills a single child process by PID. Used as the non-Unix
// fallback terminator.
type pidTerminator struct {
	pid int
}

func (t pidTerminator) kill() error {
	proc, err := os.FindProcess(t.pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}

// pgidTerminator mirrors the Unix terminator name so shared code compiles; it
// kills the child PID directly on non-Unix platforms.
type pgidTerminator struct {
	pgid int
}

func (t pgidTerminator) kill() error {
	return pidTerminator{pid: t.pgid}.kill()
}

// sysProcAttrDetached returns nil on non-Unix platforms (no session detach).
func sysProcAttrDetached() *syscall.SysProcAttr { return nil }
