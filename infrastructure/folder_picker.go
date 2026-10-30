package infrastructure

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/UrielJaloto/music-ops/services"
)

const (
	lastFolderFileName  = "last_folder.txt"
	attachedConsoleOnly = 1
	exitPrompt          = "Press Enter to exit"
)

type FolderPicker struct {
	settingsFolder string
	browse         func(title, start string) (string, error)
}

func NewFolderPicker(settingsFolder string) FolderPicker {
	return FolderPicker{settingsFolder: settingsFolder, browse: browseFolder}
}

func (picker FolderPicker) ChooseFolder(ctx context.Context, title, start string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if start == "" {
		start = picker.lastFolder()
	}
	folder, err := picker.browse(title, start)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(folder) == "" {
		return "", services.ErrFolderChoiceCancelled
	}
	return folder, picker.rememberFolder(folder)
}

func (picker FolderPicker) lastFolder() string {
	data, err := os.ReadFile(filepath.Clean(filepath.Join(picker.settingsFolder, lastFolderFileName)))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (picker FolderPicker) rememberFolder(folder string) error {
	if err := os.MkdirAll(picker.settingsFolder, configDirPermission); err != nil {
		return fmt.Errorf("remembering last folder: %w", err)
	}
	if err := os.WriteFile(filepath.Join(picker.settingsFolder, lastFolderFileName), []byte(folder), configFilePermission); err != nil {
		return fmt.Errorf("remembering last folder: %w", err)
	}
	return nil
}

func readFolderLine(input io.Reader, output io.Writer, title string) (string, error) {
	if _, err := fmt.Fprintf(output, "%s: ", title); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

type ConsoleSession struct {
	processCount func() (int, error)
	input        io.Reader
	output       io.Writer
}

func NewConsoleSession(input io.Reader, output io.Writer) ConsoleSession {
	return ConsoleSession{processCount: consoleProcessCount, input: input, output: output}
}

func (session ConsoleSession) WaitForExit() error {
	count, err := session.processCount()
	if err != nil || count != attachedConsoleOnly {
		return nil
	}
	if _, err := fmt.Fprint(session.output, exitPrompt); err != nil {
		return err
	}
	_, err = bufio.NewReader(session.input).ReadString('\n')
	if errors.Is(err, io.EOF) || errors.Is(err, fs.ErrClosed) {
		return nil
	}
	return err
}
