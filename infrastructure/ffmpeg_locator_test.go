package infrastructure

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/services"
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

func TestLocateFFmpeg_SearchOrder(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	localAppData := t.TempDir()
	packages := filepath.Join(localAppData, "Microsoft", "WinGet", "Packages")
	older := createExecutable(t, filepath.Join(packages, "Gyan.FFmpeg_1", "ffmpeg-7.0", "bin", executableFileName("ffmpeg")), base)
	newer := createExecutable(t, filepath.Join(packages, "Gyan.FFmpeg_2", "ffmpeg-8.0", "bin", executableFileName("ffmpeg")), base.Add(time.Hour))
	createExecutable(t, filepath.Join(packages, "Other.Tool", "x", "bin", executableFileName("ffmpeg")), base.Add(2*time.Hour))
	linkFolder := filepath.Join(localAppData, "Microsoft", "WinGet", "Links")
	configured := createExecutable(t, filepath.Join(t.TempDir(), "custom-ffmpeg.exe"), base)
	cases := []struct {
		name       string
		configured string
		lookPath   map[string]string
		hasLink    bool
		expected   string
	}{
		{name: "ConfiguredWins", configured: configured, lookPath: map[string]string{"ffmpeg": "path-ffmpeg"}, hasLink: true, expected: configured},
		{name: "PathBeforeLinks", lookPath: map[string]string{"ffmpeg": "path-ffmpeg"}, hasLink: true, expected: "path-ffmpeg"},
		{name: "LinksBeforePackages", hasLink: true, expected: filepath.Join(linkFolder, executableFileName("ffmpeg"))},
		{name: "NewestPackage", expected: newer},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			link := filepath.Join(linkFolder, executableFileName("ffmpeg"))
			_ = os.Remove(link)
			if testCase.hasLink {
				createExecutable(t, link, base)
			}

			path, err := LocateFFmpeg(testCase.configured, lookPathFinding(testCase.lookPath), localAppData)

			if err != nil || path != testCase.expected || path == older {
				t.Fatalf("path = %s err = %v", path, err)
			}
		})
	}
}

func TestLocateFFprobe_UsesItsOwnName(t *testing.T) {
	path, err := LocateFFprobe("", lookPathFinding(map[string]string{"ffprobe": "path-ffprobe", "ffmpeg": "path-ffmpeg"}), t.TempDir())

	if err != nil || path != "path-ffprobe" {
		t.Fatalf("path = %s err = %v", path, err)
	}
}

func TestLocateFFmpeg_Missing(t *testing.T) {
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
			_, err := LocateFFmpeg(testCase.configured, lookPathFinding(testCase.lookPath), t.TempDir())

			if !errors.Is(err, services.ErrFFmpegMissing) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestFFmpegForDiagnostics(t *testing.T) {
	found := FFmpegForDiagnostics("", lookPathFinding(map[string]string{"ffmpeg": "path-ffmpeg"}), t.TempDir())
	missing := FFmpegForDiagnostics("", lookPathFinding(nil), t.TempDir())

	if found != "path-ffmpeg" || missing != executableFileName("ffmpeg") {
		t.Fatalf("found = %s missing = %s", found, missing)
	}
}
