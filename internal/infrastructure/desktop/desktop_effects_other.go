//go:build !windows

package desktop

import "fmt"

func openPath(path string) error {
	return fmt.Errorf("opening %s: %w", path, ErrUnsupportedPlatform)
}

func playFinishSound() error {
	return ErrUnsupportedPlatform
}
