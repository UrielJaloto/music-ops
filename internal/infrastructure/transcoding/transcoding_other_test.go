//go:build !windows

package transcoding

import (
	"strings"
	"testing"
)

func TestToProcessPath_LongPathIsUnchanged(t *testing.T) {
	path := "/" + strings.Repeat("a", 300)

	if toProcessPath(path) != path {
		t.Fatal("path changed")
	}
}
