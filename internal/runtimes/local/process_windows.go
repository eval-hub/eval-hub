//go:build windows

package local

import (
	"os"
	"os/exec"
	"strconv"
)

// killProcessGroup kills the process and its entire child tree using
// taskkill /T /F. Falls back to os.FindProcess + Kill if taskkill fails.
func killProcessGroup(pid int) error {
	err := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
	if err == nil {
		return nil
	}

	// Fall back to killing just the direct process.
	p, findErr := os.FindProcess(pid)
	if findErr != nil {
		return err
	}
	if killErr := p.Kill(); killErr != nil {
		return err
	}
	return nil
}

// setSysProcAttr is a no-op on Windows (no Setpgid equivalent).
func setSysProcAttr(cmd *exec.Cmd) {}
