//go:build windows

package process

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

func hideProcessWindow(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
