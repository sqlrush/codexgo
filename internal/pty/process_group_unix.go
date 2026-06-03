//go:build unix

package pty

import (
	"errors"
	"syscall"
)

// killProcessGroup sends SIGKILL to the entire process group identified by pgid
// (best-effort). A missing group (ESRCH) is treated as success. Mirrors
// kill_process_group in process_group.rs.
func killProcessGroup(pgid int) error {
	err := syscall.Kill(-pgid, syscall.SIGKILL)
	if err == nil || errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// pgidTerminator kills a known process group on terminate. On Unix the child is
// started as a session/group leader (Setsid), so its PID equals its PGID and we
// can hard-kill the whole group, matching the pipe/PTY backends in codex.
type pgidTerminator struct {
	pgid int
}

func (t pgidTerminator) kill() error {
	return killProcessGroup(t.pgid)
}

// sysProcAttrDetached returns SysProcAttr that starts the child in a new session
// so it does not inherit the controlling TTY. Used by the pipe backend, matching
// detach_from_tty in process_group.rs.
func sysProcAttrDetached() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
