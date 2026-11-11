//go:build !windows

package desktop

import (
	"errors"
	"testing"
)

func TestDesktopEffects_Unsupported(t *testing.T) {
	if err := openPath("/tmp/report.md"); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("open err = %v", err)
	}
	if err := playFinishSound(); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("sound err = %v", err)
	}
}
