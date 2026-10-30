//go:build windows

package infrastructure

import "golang.org/x/sys/windows"

func freeBytes(folder string) (uint64, error) {
	path, err := windows.UTF16PtrFromString(folder)
	if err != nil {
		return 0, err
	}
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(path, &available, &total, &free); err != nil {
		return 0, err
	}
	return available, nil
}
