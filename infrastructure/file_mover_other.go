//go:build !windows

package infrastructure

import "syscall"

const crossDeviceErrno = syscall.EXDEV
