//go:build windows

package filesystem

import "syscall"

const crossDeviceErrno = syscall.Errno(17)
