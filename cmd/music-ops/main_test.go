package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UrielJaloto/music-ops/infrastructure"
	"github.com/UrielJaloto/music-ops/presentation"
	"github.com/UrielJaloto/music-ops/services"
)

func testEnvironment(t *testing.T) func(string) (string, bool) {
	t.Helper()
	isolated := map[string]string{"APPDATA": filepath.Join(t.TempDir(), "appdata")}
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
		{name: "UnknownOverride", arguments: []string{"doctor", "--set", "output.color=blue"}, expectedCode: 1, expectedStderr: "error: unknown setting; run: music-ops config show\n"},
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
	environment := testEnvironment(t)
	paths := infrastructure.ReadEnvironmentPaths(environment)
	store := infrastructure.NewConfigStore(infrastructure.DefaultConfigPath(paths.AppData), environment)
	overrides := map[string]string{"output.dir": t.TempDir(), "lastfm.base_url": "http://127.0.0.1:1/", "advisor.port": "1099"}
	config, err := store.Load(context.Background(), overrides)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	application := newWiring(wiringSettings{config: config, paths: paths, request: presentation.RouterRequest{Command: "run"}, console: terminal{input: strings.NewReader(""), output: &stdout, errors: &stderr}, environment: environment, store: store})
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
	if application.doctorCommand() == nil || application.configCommand() == nil || application.advisorCommand() == nil {
		t.Fatal("command missing")
	}
}

func TestWiring_SecondImportStackReportsBusyLibrary(t *testing.T) {
	application := newTestWiring(t)

	_, firstErr := application.newImportController(context.Background())
	_, secondErr := application.newImportController(context.Background())

	if firstErr != nil || !errors.Is(secondErr, services.ErrLibraryBusy) {
		t.Fatalf("errors = %v %v", firstErr, secondErr)
	}
}
