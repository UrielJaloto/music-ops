//go:build windows

package transcoding

import (
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestLocateFFmpeg_WingetSearchOrder(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)
	packages := filepath.Join(localAppData, "Microsoft", "WinGet", "Packages")
	createExecutable(t, filepath.Join(packages, "Gyan.FFmpeg_1", "ffmpeg-7.0", "bin", "ffmpeg.exe"), base)
	newer := createExecutable(t, filepath.Join(packages, "Gyan.FFmpeg_2", "ffmpeg-8.0", "bin", "ffmpeg.exe"), base.Add(time.Hour))
	createExecutable(t, filepath.Join(packages, "Other.Tool", "x", "bin", "ffmpeg.exe"), base.Add(2*time.Hour))
	link := filepath.Join(localAppData, "Microsoft", "WinGet", "Links", "ffmpeg.exe")

	fromPackages, packagesErr := LocateFFmpeg("", lookPathFinding(nil))
	createExecutable(t, link, base)
	fromLinks, linksErr := LocateFFmpeg("", lookPathFinding(nil))
	fromPath, pathErr := LocateFFmpeg("", lookPathFinding(map[string]string{"ffmpeg": "path-ffmpeg"}))

	if packagesErr != nil || fromPackages != newer {
		t.Fatalf("packages: path = %s err = %v", fromPackages, packagesErr)
	}
	if linksErr != nil || fromLinks != link {
		t.Fatalf("links: path = %s err = %v", fromLinks, linksErr)
	}
	if pathErr != nil || fromPath != "path-ffmpeg" {
		t.Fatalf("path: path = %s err = %v", fromPath, pathErr)
	}
}

func TestFDKLibraryCandidates_FreacUnderProgramFiles(t *testing.T) {
	programFiles, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, 0)
	if err != nil {
		t.Fatal(err)
	}

	candidates := listFDKLibraryCandidates()

	if len(candidates) != 1 || candidates[0] != filepath.Join(programFiles, "freac", "codecs", "FDK-AAC.dll") {
		t.Fatalf("candidates = %v", candidates)
	}
}

func TestListPackagedExecutables_BadPattern_FindsNothing(t *testing.T) {
	if candidates := listPackagedExecutables("in[valid", "ffmpeg.exe"); len(candidates) != 0 {
		t.Fatalf("candidates = %v", candidates)
	}
}

func TestCompareNewestFirst_MissingFiles_AreEqual(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")

	if compareNewestFirst(missing, missing) != 0 {
		t.Fatal("missing files compared")
	}
}

func TestListInstalledExecutables_UnknownCacheFolder(t *testing.T) {
	t.Setenv("LOCALAPPDATA", "")

	if found := listInstalledExecutables("ffmpeg.exe"); found != nil {
		t.Fatalf("found = %v", found)
	}
}
