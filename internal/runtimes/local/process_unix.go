//go:build !windows

package local

import (
	"os/exec"
	"syscall"
)

// killProcessGroup sends SIGKILL to the entire process group identified by pid.
func killProcessGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}

// setSysProcAttr configures the command to start in its own process group so
// that killProcessGroup can target the whole subprocess tree.
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
