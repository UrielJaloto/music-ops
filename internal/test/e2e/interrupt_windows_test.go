//go:build (e2e || live) && windows

package e2e

import (
	"os/exec"
	"syscall"
)

var generateConsoleCtrlEvent = syscall.NewLazyDLL("kernel32.dll").NewProc("GenerateConsoleCtrlEvent")

func startInOwnProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

func (running *runningProcess) interrupt() error {
	isSent, _, err := generateConsoleCtrlEvent.Call(syscall.CTRL_BREAK_EVENT, uintptr(running.command.Process.Pid))
	if isSent == 0 {
		return err
	}
	return nil
}
