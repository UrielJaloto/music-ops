//go:build integration

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UrielJaloto/music-ops/internal/infrastructure/storage"
)

func userFolders(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("APPDATA", filepath.Join(root, "appdata"))
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "localappdata"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "appdata"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "localappdata"))
	t.Setenv("HOME", root)
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(cache, "music-ops")
}

func runCommand(t *testing.T, arguments ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(arguments, terminal{input: strings.NewReader(""), output: &stdout, errors: &stderr}, os.LookupEnv)
	return code, stdout.String(), stderr.String()
}

func offline(arguments ...string) []string {
	return append(arguments, "--set", "lastfm.base_url=http://127.0.0.1:1/", "--set", "advisor.port=1099")
}

func TestRun_Version(t *testing.T) {
	userFolders(t)

	code, stdout, stderr := runCommand(t, "version")

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if code != 0 || len(lines) != 3 || lines[0] != "music-ops dev" || !strings.HasPrefix(lines[1], "ffmpeg ") || !strings.HasSuffix(lines[2], " kb/s") || stderr != "" {
		t.Fatalf("code = %d stdout = %q stderr = %q", code, stdout, stderr)
	}
}

func TestRun_Version_WithoutFFmpeg(t *testing.T) {
	userFolders(t)
	missing := filepath.Join(t.TempDir(), "ffmpeg.exe")

	code, stdout, _ := runCommand(t, "version", "--set", "encoder.ffmpeg="+missing)

	if code != 0 || stdout != "music-ops dev\nffmpeg not found\n" {
		t.Fatalf("code = %d stdout = %q", code, stdout)
	}
}

func TestRun_MissingFFmpeg(t *testing.T) {
	userFolders(t)
	missing := filepath.Join(t.TempDir(), "ffmpeg.exe")
	input := t.TempDir()
	writeFile(t, filepath.Join(input, "track.flac"))

	code, _, stderr := runCommand(t, offline("run", input, "--set", "encoder.ffmpeg="+missing, "--out", t.TempDir())...)

	if code != 1 || !strings.Contains(stderr, "winget install -e --id Gyan.FFmpeg") {
		t.Fatalf("code = %d stderr = %q", code, stderr)
	}
}

func TestRun_RunInProgress_FailsBeforeTheFolderDialog(t *testing.T) {
	workDir := userFolders(t)
	if err := os.MkdirAll(workDir, 0o750); err != nil {
		t.Fatal(err)
	}
	busy := storage.NewBoltStore(filepath.Join(workDir, stateFileName))
	if err := busy.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = busy.Close() })

	code, _, stderr := runCommand(t, offline("run", "--out", t.TempDir())...)

	if code != 1 || stderr != "error: another music-ops run is in progress\n" {
		t.Fatalf("code = %d stderr = %q", code, stderr)
	}
}

func TestRun_WorkFolderFailures(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, workDir string)
	}{
		{name: "WorkFolderUnknown", prepare: func(t *testing.T, _ string) {
			t.Setenv("LOCALAPPDATA", "")
			t.Setenv("XDG_CACHE_HOME", "")
			t.Setenv("HOME", "")
		}},
		{name: "StateFileIsAFolder", prepare: func(t *testing.T, workDir string) {
			if err := os.MkdirAll(filepath.Join(workDir, stateFileName), 0o750); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "ReportsFolderIsAFile", prepare: func(t *testing.T, workDir string) { writeFile(t, filepath.Join(workDir, "runs")) }},
		{name: "StagingFolderIsAFile", prepare: func(t *testing.T, workDir string) { writeFile(t, filepath.Join(workDir, "staging")) }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.prepare(t, userFolders(t))

			code, _, stderr := runCommand(t, offline("run", t.TempDir(), "--out", t.TempDir())...)

			if code != 1 || !strings.HasPrefix(stderr, "error: ") {
				t.Fatalf("code = %d stderr = %q", code, stderr)
			}
		})
	}
}

func TestRun_Status_ReportsUnreachableLastFM(t *testing.T) {
	userFolders(t)

	code, stdout, _ := runCommand(t, offline("status", "--out", t.TempDir())...)

	if code != 1 || !strings.Contains(stdout, "last.fm") || !strings.Contains(stdout, "required check(s) failed") {
		t.Fatalf("code = %d stdout = %q", code, stdout)
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
			userFolders(t)

			code, _, stderr := runCommand(t, testCase.arguments...)

			if code != testCase.expectedCode || stderr != testCase.expectedStderr {
				t.Fatalf("code = %d stderr = %q", code, stderr)
			}
		})
	}
}

func TestRun_ConfigAndUsage(t *testing.T) {
	userFolders(t)

	pathCode, pathOut, _ := runCommand(t, "config", "path")
	unknownCode, unknownOut, _ := runCommand(t, "explode")

	if pathCode != 0 || !strings.HasSuffix(strings.TrimSpace(pathOut), filepath.Join("music-ops", "config.yaml")) {
		t.Fatalf("config path code = %d output = %q", pathCode, pathOut)
	}
	if unknownCode != 1 || !strings.HasPrefix(unknownOut, "usage: music-ops") {
		t.Fatalf("unknown code = %d output = %q", unknownCode, unknownOut)
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
