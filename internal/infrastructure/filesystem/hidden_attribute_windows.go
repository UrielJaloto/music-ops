//go:build windows

package filesystem

import (
	"io/fs"
	"syscall"
)

func isHiddenEntry(entry fs.DirEntry) bool {
	info, err := entry.Info()
	return err == nil && hasHiddenAttribute(info)
}

func hasHiddenAttribute(info fs.FileInfo) bool {
	attributes, hasAttributes := info.Sys().(*syscall.Win32FileAttributeData)
	return hasAttributes && attributes.FileAttributes&syscall.FILE_ATTRIBUTE_HIDDEN != 0
}
