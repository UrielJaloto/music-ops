//go:build !windows

package infrastructure

import "syscall"

func freeBytes(folder string) (uint64, error) {
	var stats syscall.Statfs_t
	if err := syscall.Statfs(folder, &stats); err != nil {
		return 0, err
	}
	return stats.Bavail * uint64(stats.Bsize), nil
}
