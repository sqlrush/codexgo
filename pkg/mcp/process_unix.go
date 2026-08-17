//go:build unix

package mcp

import (
	"os/exec"
	"syscall"
	"time"
)

// processTermGracePeriod is the delay between a graceful SIGTERM to the process
// group and the escalation SIGKILL, matching the reference launcher's 2-second
// grace period.
const processTermGracePeriod = 2 * time.Second

// configureProcessGroup places the child in its own process group so the whole
// tree can be signalled together, matching the reference's process_group(0).
func configureProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// terminateProcess signals the child's process group with SIGTERM and escalates
// to SIGKILL after a grace period, then reaps the process. A negative pid value
// targets the process group.
func terminateProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pgid := cmd.Process.Pid
	// Best-effort graceful termination of the whole group.
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		// Group may already be gone; fall back to killing the leader directly.
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}

	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(processTermGracePeriod):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = cmd.Process.Kill()
		<-done
		return nil
	}
}
