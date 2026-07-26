//go:build windows

package infrastructure

import (
	"io/fs"
	"syscall"
)

func isHiddenEntry(entry fs.DirEntry) bool {
	info, err := entry.Info()
	if err != nil {
		return false
	}
	attributes, hasAttributes := info.Sys().(*syscall.Win32FileAttributeData)
	return hasAttributes && attributes.FileAttributes&syscall.FILE_ATTRIBUTE_HIDDEN != 0
}
