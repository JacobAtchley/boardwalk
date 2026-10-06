//go:build !windows

package ui

import (
	"os/exec"
	"syscall"
)

// shellCommand runs script with sh, in a process group of its own. The
// group is what keeps the command alive past boardwalk: a ctrl+c that quits
// boardwalk is delivered by the terminal to its foreground group, and the
// command is no longer in it.
func shellCommand(script string) *exec.Cmd {
	cmd := exec.Command("sh", "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}
