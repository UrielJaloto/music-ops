//go:build !windows

package infrastructure

import "io/fs"

func isHiddenEntry(fs.DirEntry) bool {
	return false
}
