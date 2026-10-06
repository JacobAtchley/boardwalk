//go:build windows

package ui

import (
	"os/exec"
	"syscall"
)

// shellCommand runs script with cmd, in a process group of its own, for the
// reason the unix version gives. The CmdLine field builds the full command
// line ourselves because exec.Command escapes quotes that cmd doesn't
// understand; with /C and a quoted whole command, cmd strips the outer
// quotes and runs the rest as typed.
func shellCommand(script string) *exec.Cmd {
	cmd := exec.Command("cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       `cmd /C "` + script + `"`,
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
	return cmd
}
