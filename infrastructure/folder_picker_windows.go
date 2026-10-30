//go:build windows

package infrastructure

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/sqweek/dialog"
	"golang.org/x/sys/windows"

	"github.com/UrielJaloto/music-ops/services"
)

const consoleProcessSlots = 4

var getConsoleProcessList = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList")

func browseFolder(title, start string) (string, error) {
	folder, err := dialog.Directory().Title(title).SetStartDir(start).Browse()
	if errors.Is(err, dialog.ErrCancelled) {
		return "", services.ErrFolderChoiceCancelled
	}
	if err != nil {
		return "", fmt.Errorf("choosing folder: %w", err)
	}
	return folder, nil
}

func consoleProcessCount() (int, error) {
	processes := make([]uint32, consoleProcessSlots)
	count, _, err := getConsoleProcessList.Call(uintptr(unsafe.Pointer(&processes[0])), consoleProcessSlots)
	if count == 0 {
		return 0, fmt.Errorf("reading console process list: %w", err)
	}
	return int(count), nil
}
