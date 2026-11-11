//go:build !windows

package filesystem

import "io/fs"

func isHiddenEntry(fs.DirEntry) bool {
	return false
}
