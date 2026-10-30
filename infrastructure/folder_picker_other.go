//go:build !windows

package infrastructure

import "os"

const detachedConsoleCount = 2

func browseFolder(title, _ string) (string, error) {
	return readFolderLine(os.Stdin, os.Stdout, title)
}

func consoleProcessCount() (int, error) {
	return detachedConsoleCount, nil
}
