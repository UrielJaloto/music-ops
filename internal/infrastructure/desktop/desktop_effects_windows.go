//go:build windows

package desktop

import (
	"fmt"

	"golang.org/x/sys/windows"
)

const (
	openVerb         = "open"
	asteriskSound    = 0x40
	messageBeepFails = 0
)

var messageBeep = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBeep")

func openPath(path string) error {
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	if err := windows.ShellExecute(0, windows.StringToUTF16Ptr(openVerb), file, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	return nil
}

func playFinishSound() error {
	succeeded, _, err := messageBeep.Call(asteriskSound)
	if succeeded == messageBeepFails {
		return fmt.Errorf("playing the finish sound: %w", err)
	}
	return nil
}
