//go:build !windows

package process

import "os/exec"

func hideProcessWindow(*exec.Cmd) {}
