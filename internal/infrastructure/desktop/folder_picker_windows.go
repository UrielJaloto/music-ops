//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/UrielJaloto/music-ops/internal/services/importing"
	"github.com/sqweek/dialog"
	"golang.org/x/sys/windows"
)

const consoleProcessSlots = 4

var getConsoleProcessList = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList")

func browseFolder(title, start string) (string, error) {
	folder, err := dialog.Directory().Title(title).SetStartDir(start).Browse()
	return folder, wrapBrowseError(err)
}

func wrapBrowseError(err error) error {
	if errors.Is(err, dialog.ErrCancelled) {
		return importing.ErrFolderChoiceCancelled
	}
	if err != nil {
		return fmt.Errorf("choosing folder: %w", err)
	}
	return nil
}

func countConsoleProcesses() (int, error) {
	processes := make([]uint32, consoleProcessSlots)
	count, _, err := getConsoleProcessList.Call(uintptr(unsafe.Pointer(&processes[0])), consoleProcessSlots)
	if count == 0 {
		return 0, fmt.Errorf("reading console process list: %w", err)
	}
	return int(count), nil
}
