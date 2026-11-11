//go:build windows && integration

package desktop

import (
	"errors"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestOpenPath_MissingPath_ReturnsShellError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing", "report.md")

	err := openPath(missing)

	if !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || !strings.Contains(err.Error(), missing) {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenPath_PathWithNul_ReturnsError(t *testing.T) {
	err := openPath("bad\x00path")

	if !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("err = %v", err)
	}
}

func TestPlayFinishSound_Succeeds(t *testing.T) {
	if err := playFinishSound(); err != nil {
		t.Fatalf("err = %v", err)
	}
}
