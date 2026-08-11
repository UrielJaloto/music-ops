//go:build windows

package transcoding

import (
	"path/filepath"
	"strings"
)

const (
	longPathLimit  = 240
	longPathPrefix = `\\?\`
)

func toProcessPath(path string) string {
	if len(path) <= longPathLimit || strings.HasPrefix(path, longPathPrefix) {
		return path
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return longPathPrefix + absolute
}
