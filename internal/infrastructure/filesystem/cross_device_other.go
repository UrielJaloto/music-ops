//go:build !windows

package filesystem

import "syscall"

const crossDeviceErrno = syscall.EXDEV
