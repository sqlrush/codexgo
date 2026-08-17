//go:build windows

package mcp

import "os/exec"

// configureProcessGroup is a no-op on Windows; the reference launcher relies on
// taskkill /T to terminate the process tree instead of a Unix process group.
func configureProcessGroup(cmd *exec.Cmd) {}

// terminateProcess kills the child process tree with taskkill, matching the
// reference launcher's Windows behavior.
func terminateProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid
	kill := exec.Command("taskkill", "/PID", itoa(pid), "/T", "/F")
	_ = kill.Run()
	_, _ = cmd.Process.Wait()
	return nil
}

// itoa converts an int to its decimal string without pulling in strconv at the
// call site, keeping this Windows-only file dependency-light.
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
