package transcoding

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/internal/services/diagnostics"
)

func lookPathFinding(found map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		if path, isFound := found[name]; isFound {
			return path, nil
		}
		return "", exec.ErrNotFound
	}
}

func createExecutable(t *testing.T, path string, modifiedAt time.Time) string {
	t.Helper()
	writeTestFile(t, path, "binary")
	if err := os.Chtimes(path, modifiedAt, modifiedAt); err != nil {
		t.Fatal(err)
	}
	return path
}

func isolateToolFolders(t *testing.T) {
	t.Helper()
	empty := t.TempDir()
	t.Setenv("LOCALAPPDATA", empty)
	t.Setenv("XDG_CACHE_HOME", empty)
	t.Setenv("HOME", empty)
}

func TestLocateFFmpeg_ConfiguredBeforePath(t *testing.T) {
	configured := createExecutable(t, filepath.Join(t.TempDir(), "custom-ffmpeg"), time.Now())
	cases := []struct {
		name       string
		configured string
		expected   string
	}{
		{name: "ConfiguredWins", configured: configured, expected: configured},
		{name: "PathWhenNotConfigured", expected: "path-ffmpeg"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path, err := LocateFFmpeg(testCase.configured, lookPathFinding(map[string]string{"ffmpeg": "path-ffmpeg"}))

			if err != nil || path != testCase.expected {
				t.Fatalf("path = %s err = %v", path, err)
			}
		})
	}
}

func TestLocateFFprobe_UsesItsOwnName(t *testing.T) {
	path, err := LocateFFprobe("", lookPathFinding(map[string]string{"ffprobe": "path-ffprobe", "ffmpeg": "path-ffmpeg"}))

	if err != nil || path != "path-ffprobe" {
		t.Fatalf("path = %s err = %v", path, err)
	}
}

func TestLocateFFmpeg_Missing(t *testing.T) {
	isolateToolFolders(t)
	cases := []struct {
		name       string
		configured string
		lookPath   map[string]string
	}{
		{name: "NothingInstalled"},
		{name: "ConfiguredPathMissing", configured: filepath.Join(t.TempDir(), "nope.exe"), lookPath: map[string]string{"ffmpeg": "path-ffmpeg"}},
		{name: "ConfiguredPathIsFolder", configured: t.TempDir(), lookPath: map[string]string{"ffmpeg": "path-ffmpeg"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := LocateFFmpeg(testCase.configured, lookPathFinding(testCase.lookPath))

			if !errors.Is(err, diagnostics.ErrFFmpegMissing) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestLocateFDKLibrary_ConfiguredWinsEvenWhenMissing(t *testing.T) {
	configured := filepath.Join(t.TempDir(), "custom-fdk")

	if path := LocateFDKLibrary(configured); path != configured {
		t.Fatalf("path = %s", path)
	}
}

func TestFindFirstFile_SkipsMissingAndFolders(t *testing.T) {
	folder := t.TempDir()
	file := createExecutable(t, filepath.Join(folder, "tool"), time.Now())
	cases := []struct {
		name       string
		candidates []string
		expected   string
	}{
		{name: "None"},
		{name: "OnlyMissingAndFolder", candidates: []string{filepath.Join(folder, "missing"), folder}},
		{name: "FileAfterMissing", candidates: []string{filepath.Join(folder, "missing"), folder, file}, expected: file},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if path := findFirstFile(testCase.candidates); path != testCase.expected {
				t.Fatalf("path = %q", path)
			}
		})
	}
}
