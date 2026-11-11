//go:build (e2e || live) && !windows

package e2e

import (
	"os"
	"os/exec"
)

func startInOwnProcessGroup(*exec.Cmd) {}

func (running *runningProcess) interrupt() error {
	return running.command.Process.Signal(os.Interrupt)
}
