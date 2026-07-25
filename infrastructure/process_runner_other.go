//go:build !windows

package infrastructure

import "os/exec"

func hideProcessWindow(*exec.Cmd) {}
