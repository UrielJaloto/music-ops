//go:build !windows

package desktop

import "os"

const detachedConsoleCount = 2

func browseFolder(title, _ string) (string, error) {
	return readFolderLine(os.Stdin, os.Stdout, title)
}

func countConsoleProcesses() (int, error) {
	return detachedConsoleCount, nil
}
