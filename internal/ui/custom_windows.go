//go:build windows

package ui

import (
	"os/exec"
	"syscall"
)

// shellCommand runs script with cmd, in a process group of its own, for the
// reason the unix version gives.
func shellCommand(script string) *exec.Cmd {
	cmd := exec.Command("cmd", "/C", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	return cmd
}
