//go:build windows

package ui

import (
	"os/exec"
	"syscall"
)

// shellCommand runs script with cmd, in a process group of its own, for the
// reason the unix version gives. The CmdLine field builds the full command
// line ourselves because exec.Command escapes quotes that cmd doesn't
// understand. With /S and a quoted whole command, cmd always strips exactly
// the outer quotes and runs the rest as typed; without /S it keeps them when
// the script is a single quoted path containing spaces.
func shellCommand(script string) *exec.Cmd {
	cmd := exec.Command("cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       `cmd /S /C "` + script + `"`,
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
	return cmd
}
