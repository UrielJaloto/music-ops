package desktop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/UrielJaloto/music-ops/internal/infrastructure/storage"
	"github.com/UrielJaloto/music-ops/internal/services/importing"
)

type shellEffects struct {
	soundErr error
	openErr  error
	opened   []string
	sounds   int
}

func (effects *shellEffects) shell() DesktopShell {
	shell := NewDesktopShell(storage.NewReportStorage("E:/Work"))
	shell.open = func(path string) error {
		effects.opened = append(effects.opened, path)
		return effects.openErr
	}
	shell.playSound = func() error {
		effects.sounds++
		return effects.soundErr
	}
	return shell
}

func TestDesktopShell_Finish(t *testing.T) {
	errSound := errors.New("sound")
	errOpen := errors.New("open")
	cases := []struct {
		name          string
		soundErr      error
		openErr       error
		expectedError error
	}{
		{name: "AllEffectsSucceed"},
		{name: "SoundUnsupported_Ignored", soundErr: ErrUnsupportedPlatform},
		{name: "SoundFails_OpensStillRun", soundErr: errSound, expectedError: errSound},
		{name: "OpenFails_Reported", openErr: errOpen, expectedError: errOpen},
		{name: "OpenUnsupported_Reported", openErr: ErrUnsupportedPlatform, expectedError: ErrUnsupportedPlatform},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			effects := &shellEffects{soundErr: testCase.soundErr, openErr: testCase.openErr}

			err := effects.shell().Finish(context.Background(), importing.ImportOutput{RunID: "r1", Output: "E:/Music"})

			if testCase.expectedError == nil && err != nil || !errors.Is(err, testCase.expectedError) {
				t.Fatalf("err = %v, want %v", err, testCase.expectedError)
			}
			expectedOpened := []string{filepath.Join("E:/Work", "runs", "r1", "report.md"), "E:/Music"}
			if diff := differences(expectedOpened, effects.opened); diff != "" || effects.sounds != 1 {
				t.Fatalf("sounds = %d opened (-want +got):\n%s", effects.sounds, diff)
			}
		})
	}
}

func TestFolderPicker_RemembersLastFolder(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "music-ops")
	var starts []string
	picker := FolderPicker{settingsFolder: settings, browse: func(_, start string) (string, error) {
		starts = append(starts, start)
		return "D:/Downloads/Album", nil
	}}

	first, firstErr := picker.ChooseFolder(context.Background(), "Choose", "")
	second, secondErr := picker.ChooseFolder(context.Background(), "Choose", "")
	explicit, _ := picker.ChooseFolder(context.Background(), "Choose", "C:/Start")

	if firstErr != nil || secondErr != nil || first != "D:/Downloads/Album" || second != first || explicit != first {
		t.Fatalf("folders = %s %s errors = %v %v", first, second, firstErr, secondErr)
	}
	if diff := differences([]string{"", "D:/Downloads/Album", "C:/Start"}, starts); diff != "" {
		t.Fatalf("starts (-want +got):\n%s", diff)
	}
}

func TestFolderPicker_Cancelled(t *testing.T) {
	cases := []struct {
		name   string
		browse func(string, string) (string, error)
	}{
		{name: "DialogCancelled", browse: func(string, string) (string, error) { return "", importing.ErrFolderChoiceCancelled }},
		{name: "EmptyAnswer", browse: func(string, string) (string, error) { return "  ", nil }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			settings := filepath.Join(t.TempDir(), "music-ops")
			picker := FolderPicker{settingsFolder: settings, browse: testCase.browse}

			_, err := picker.ChooseFolder(context.Background(), "Choose", "")

			if !errors.Is(err, importing.ErrFolderChoiceCancelled) || pathExists(filepath.Join(settings, lastFolderFileName)) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestFolderPicker_Errors(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	writeTestFile(t, blocker, "x")
	picker := FolderPicker{settingsFolder: filepath.Join(blocker, "music-ops"), browse: func(string, string) (string, error) { return "D:/A", nil }}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, rememberErr := picker.ChooseFolder(context.Background(), "Choose", "")
	_, cancelErr := picker.ChooseFolder(ctx, "Choose", "")

	if rememberErr == nil || !errors.Is(cancelErr, context.Canceled) {
		t.Fatalf("errors = %v %v", rememberErr, cancelErr)
	}
}

func TestReadFolderLine(t *testing.T) {
	var output bytes.Buffer

	folder, err := readFolderLine(strings.NewReader("  D:/Music Inbox \n"), &output, "Folder")

	if err != nil || folder != "D:/Music Inbox" || output.String() != "Folder: " {
		t.Fatalf("folder = %q output = %q err = %v", folder, output.String(), err)
	}
}

func TestConsoleSession_WaitForExit(t *testing.T) {
	cases := []struct {
		name           string
		count          int
		countErr       error
		expectedPrompt string
	}{
		{name: "DoubleClick_Prompts", count: 1, expectedPrompt: "Press Enter to exit"},
		{name: "Terminal_DoesNotPrompt", count: 2},
		{name: "UnknownConsole_DoesNotPrompt", countErr: errors.New("no console")},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var output bytes.Buffer
			session := ConsoleSession{processCount: func() (int, error) { return testCase.count, testCase.countErr }, input: strings.NewReader("\n"), output: &output}

			session.WaitForExit()

			if output.String() != testCase.expectedPrompt {
				t.Fatalf("output = %q", output.String())
			}
		})
	}
}

func differences(want, got any) string {
	if reflect.DeepEqual(want, got) {
		return ""
	}
	return fmt.Sprintf("want: %+v\ngot:  %+v", want, got)
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestFolderPicker_LastFolderFileIsAFolder(t *testing.T) {
	settings := t.TempDir()
	writeTestFile(t, filepath.Join(settings, lastFolderFileName, "inside"), "x")
	picker := FolderPicker{settingsFolder: settings, browse: func(string, string) (string, error) { return "D:/A", nil }}

	_, err := picker.ChooseFolder(context.Background(), "Choose", "")

	if err == nil || !strings.Contains(err.Error(), "remembering last folder") {
		t.Fatalf("err = %v", err)
	}
}
