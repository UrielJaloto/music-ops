//go:build integration

package main

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UrielJaloto/music-ops/internal/infrastructure/config"
	"github.com/UrielJaloto/music-ops/internal/presentation/cli"
	"github.com/UrielJaloto/music-ops/internal/services/importing"
)

func testEnvironment(t *testing.T) func(string) (string, bool) {
	t.Helper()
	root := t.TempDir()
	isolated := map[string]string{"APPDATA": filepath.Join(root, "appdata"), "LOCALAPPDATA": filepath.Join(root, "localappdata")}
	return func(name string) (string, bool) {
		if value, isSet := isolated[name]; isSet {
			return value, true
		}
		return os.LookupEnv(name)
	}
}

func runCommand(t *testing.T, arguments ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(arguments, terminal{input: strings.NewReader(""), output: &stdout, errors: &stderr}, testEnvironment(t))
	return code, stdout.String(), stderr.String()
}

func TestRun_Version(t *testing.T) {
	code, stdout, stderr := runCommand(t, "version")

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if code != 0 || len(lines) != 3 || lines[0] != "music-ops dev" || !strings.HasPrefix(lines[1], "ffmpeg ") || !strings.HasPrefix(lines[2], "encoder ") || stderr != "" {
		t.Fatalf("code = %d stdout = %q stderr = %q", code, stdout, stderr)
	}
}

func TestRun_MissingFFmpeg(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "ffmpeg.exe")

	code, _, stderr := runCommand(t, "run", t.TempDir(), "--set", "encoder.ffmpeg="+missing, "--out", t.TempDir())

	if code != 1 || !strings.Contains(stderr, "winget install -e --id Gyan.FFmpeg") {
		t.Fatalf("code = %d stderr = %q", code, stderr)
	}
}

func TestRun_CommandLineAndConfigErrors(t *testing.T) {
	cases := []struct {
		name           string
		arguments      []string
		expectedCode   int
		expectedStderr string
	}{
		{name: "MalformedFlag", arguments: []string{"run", "--set", "nodivider"}, expectedCode: 1, expectedStderr: "error: invalid command line; run: music-ops help\n"},
		{name: "UnknownOverride", arguments: []string{"status", "--set", "output.color=blue"}, expectedCode: 1, expectedStderr: "error: unknown setting; run: music-ops config show\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			code, _, stderr := runCommand(t, testCase.arguments...)

			if code != testCase.expectedCode || stderr != testCase.expectedStderr {
				t.Fatalf("code = %d stderr = %q", code, stderr)
			}
		})
	}
}

func TestRun_ConfigAndUsage(t *testing.T) {
	pathCode, pathOut, _ := runCommand(t, "config", "path")
	unknownCode, unknownOut, _ := runCommand(t, "explode")

	if pathCode != 0 || !strings.HasSuffix(strings.TrimSpace(pathOut), filepath.Join("music-ops", "config.yaml")) {
		t.Fatalf("config path code = %d output = %q", pathCode, pathOut)
	}
	if unknownCode != 1 || !strings.HasPrefix(unknownOut, "usage: music-ops") {
		t.Fatalf("unknown code = %d output = %q", unknownCode, unknownOut)
	}
}

func newTestWiring(t *testing.T) *wiring {
	t.Helper()
	return newTestWiringWith(t, map[string]string{})
}

func newTestWiringWith(t *testing.T, extra map[string]string) *wiring {
	t.Helper()
	environment := testEnvironment(t)
	paths := config.ReadEnvironmentPaths(environment)
	store := config.NewConfigStore(config.DefaultConfigPath(paths.AppData), environment)
	overrides := map[string]string{"output.dir": t.TempDir(), "lastfm.base_url": "http://127.0.0.1:1/", "advisor.port": "1099"}
	maps.Copy(overrides, extra)
	loaded, err := store.Load(context.Background(), overrides)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	application := newWiring(wiringSettings{config: loaded, paths: paths, request: cli.RouterRequest{Command: "run"}, console: terminal{input: strings.NewReader(""), output: &stdout, errors: &stderr}, environment: environment, store: store})
	t.Cleanup(func() { application.close(context.Background()) })
	return application
}

func TestWiring_BuildsEveryCommandGraph(t *testing.T) {
	_, importErr := newTestWiring(t).newImportController(context.Background())
	_, watchErr := newTestWiring(t).watchCommand(context.Background())
	_, versionErr := newTestWiring(t).versionCommand(context.Background())
	application := newTestWiring(t)

	if importErr != nil || watchErr != nil || versionErr != nil {
		t.Fatalf("errors = %v %v %v", importErr, watchErr, versionErr)
	}
	if application.statusCommand() == nil || application.configCommand() == nil || application.advisorCommand() == nil {
		t.Fatal("command missing")
	}
}

func TestWiring_SecondImportStackReportsBusyLibrary(t *testing.T) {
	application := newTestWiring(t)

	_, firstErr := application.newImportController(context.Background())
	_, secondErr := application.newImportController(context.Background())

	if firstErr != nil || !errors.Is(secondErr, importing.ErrRunInProgress) {
		t.Fatalf("errors = %v %v", firstErr, secondErr)
	}
}

func TestWiring_EncodingFailures(t *testing.T) {
	notExecutable := filepath.Join(t.TempDir(), "ffmpeg.exe")
	if err := os.WriteFile(notExecutable, []byte("text"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		override map[string]string
	}{
		{name: "FFprobeMissing", override: map[string]string{"encoder.ffprobe": filepath.Join(t.TempDir(), "ffprobe.exe")}},
		{name: "FFmpegDoesNotRun", override: map[string]string{"encoder.ffmpeg": notExecutable}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, previewErr := newTestWiringWith(t, testCase.override).previewCommand(context.Background())
			_, versionErr := newTestWiringWith(t, testCase.override).versionCommand(context.Background())
			_, watchErr := newTestWiringWith(t, testCase.override).watchCommand(context.Background())

			if previewErr == nil || versionErr == nil || watchErr == nil {
				t.Fatalf("errors = %v %v %v", previewErr, versionErr, watchErr)
			}
		})
	}
}

func TestWiring_StorageFailures(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, workRoot string) string
	}{
		{name: "WorkFolderUnknown", prepare: func(*testing.T, string) string { return "" }},
		{name: "WorkFolderIsAFile", prepare: func(t *testing.T, workRoot string) string {
			file := filepath.Join(t.TempDir(), "file")
			writeFile(t, file)
			return file
		}},
		{name: "StateFileIsAFolder", prepare: func(t *testing.T, workRoot string) string {
			if err := os.MkdirAll(filepath.Join(workRoot, applicationFolder, stateFileName), 0o750); err != nil {
				t.Fatal(err)
			}
			return workRoot
		}},
		{name: "ReportsFolderIsAFile", prepare: func(t *testing.T, workRoot string) string {
			writeFile(t, filepath.Join(workRoot, applicationFolder, "runs"))
			return workRoot
		}},
		{name: "InterruptedRunCannotBeRemoved", prepare: func(t *testing.T, workRoot string) string {
			lockFolder(t, filepath.Join(workRoot, applicationFolder, "staging", "20260101-000000-dead"))
			return workRoot
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			application := newTestWiring(t)
			application.settings.paths.LocalAppData = testCase.prepare(t, t.TempDir())

			_, err := application.newImportController(context.Background())

			if err == nil {
				t.Fatal("storage accepted")
			}
		})
	}
}

func TestWiring_CloseReportsFailures(t *testing.T) {
	application := newTestWiring(t)
	application.onClose(func(context.Context) error { return errors.New("close failed") })

	application.close(context.Background())

	stderr := application.settings.console.errors.(*bytes.Buffer).String()
	if !strings.Contains(stderr, "close failed") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestWiring_VerboseLogsToTheConsole(t *testing.T) {
	application := newTestWiring(t)
	application.settings.request.IsVerbose = true

	if application.consoleLog() != application.settings.console.errors {
		t.Fatal("verbose log does not reach stderr")
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func lockFolder(t *testing.T, folder string) {
	t.Helper()
	path := filepath.Join(folder, "track.m4a")
	writeFile(t, path)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
}
