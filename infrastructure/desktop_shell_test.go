package infrastructure

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/UrielJaloto/music-ops/services"
)

func TestFinishCommands_Golden(t *testing.T) {
	commands := finishCommands(`E:\Music\.music-ops\runs\r1\report.md`, `E:\Music`)

	expected := []ProcessRequest{
		{Executable: "powershell", Arguments: []string{"-NoProfile", "-Command", "[System.Media.SystemSounds]::Asterisk.Play()"}},
		{Executable: "rundll32", Arguments: []string{"url.dll,FileProtocolHandler", `E:\Music\.music-ops\runs\r1\report.md`}},
		{Executable: "rundll32", Arguments: []string{"url.dll,FileProtocolHandler", `E:\Music`}},
	}
	if diff := cmp.Diff(expected, commands); diff != "" {
		t.Fatalf("commands (-want +got):\n%s", diff)
	}
}

type failingRunner struct {
	failOn   string
	requests []ProcessRequest
}

func (runner *failingRunner) Run(_ context.Context, request ProcessRequest) (ProcessResult, error) {
	runner.requests = append(runner.requests, request)
	if request.Executable == runner.failOn {
		return ProcessResult{}, ProcessError{Executable: request.Executable, ExitCode: 1}
	}
	return ProcessResult{}, nil
}

func TestDesktopShell_Finish(t *testing.T) {
	cases := []struct {
		name      string
		failOn    string
		isFailure bool
	}{
		{name: "AllActionsSucceed"},
		{name: "SoundFails_OthersStillRun", failOn: "powershell", isFailure: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			runner := &failingRunner{failOn: testCase.failOn}
			shell := NewDesktopShell(runner, NewReportStorage("E:/Music"))

			err := shell.Finish(context.Background(), services.ImportOutput{RunID: "r1", Output: "E:/Music"})

			if (err != nil) != testCase.isFailure || len(runner.requests) != 3 {
				t.Fatalf("err = %v requests = %d", err, len(runner.requests))
			}
			if runner.requests[1].Arguments[1] != filepath.Join("E:/Music", ".music-ops", "runs", "r1", "report.md") {
				t.Fatalf("report path = %s", runner.requests[1].Arguments[1])
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
	if diff := cmp.Diff([]string{"", "D:/Downloads/Album", "C:/Start"}, starts); diff != "" {
		t.Fatalf("starts (-want +got):\n%s", diff)
	}
}

func TestFolderPicker_Cancelled(t *testing.T) {
	cases := []struct {
		name   string
		browse func(string, string) (string, error)
	}{
		{name: "DialogCancelled", browse: func(string, string) (string, error) { return "", services.ErrFolderChoiceCancelled }},
		{name: "EmptyAnswer", browse: func(string, string) (string, error) { return "  ", nil }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			settings := filepath.Join(t.TempDir(), "music-ops")
			picker := FolderPicker{settingsFolder: settings, browse: testCase.browse}

			_, err := picker.ChooseFolder(context.Background(), "Choose", "")

			if !errors.Is(err, services.ErrFolderChoiceCancelled) || pathExists(filepath.Join(settings, lastFolderFileName)) {
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

			err := session.WaitForExit()

			if err != nil || output.String() != testCase.expectedPrompt {
				t.Fatalf("output = %q err = %v", output.String(), err)
			}
		})
	}
}
