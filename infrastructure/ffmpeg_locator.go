package infrastructure

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/UrielJaloto/music-ops/services"
)

const (
	ffmpegName        = "ffmpeg"
	ffprobeName       = "ffprobe"
	windowsExecutable = ".exe"
	wingetPackageGlob = "Gyan.FFmpeg*"
)

func LocateFFmpeg(configured string, lookPath func(string) (string, error), localAppData string) (string, error) {
	return locateExecutable(executableSearch{name: ffmpegName, configured: configured, lookPath: lookPath, localAppData: localAppData})
}

func LocateFFprobe(configured string, lookPath func(string) (string, error), localAppData string) (string, error) {
	return locateExecutable(executableSearch{name: ffprobeName, configured: configured, lookPath: lookPath, localAppData: localAppData})
}

type executableSearch struct {
	name         string
	configured   string
	lookPath     func(string) (string, error)
	localAppData string
}

func locateExecutable(search executableSearch) (string, error) {
	if search.configured != "" {
		return existingFile(search.configured, search.name)
	}
	if path, err := search.lookPath(search.name); err == nil {
		return path, nil
	}
	fileName := executableFileName(search.name)
	linked := filepath.Join(search.localAppData, "Microsoft", "WinGet", "Links", fileName)
	if isFile(linked) {
		return linked, nil
	}
	return newestPackagedExecutable(search.localAppData, fileName, search.name)
}

func existingFile(path, name string) (string, error) {
	if !isFile(path) {
		return "", fmt.Errorf("%w: %s not found at %s", services.ErrFFmpegMissing, name, path)
	}
	return path, nil
}

func newestPackagedExecutable(localAppData, fileName, name string) (string, error) {
	folders, err := filepath.Glob(filepath.Join(localAppData, "Microsoft", "WinGet", "Packages", wingetPackageGlob, "*", "bin"))
	if err != nil {
		return "", fmt.Errorf("searching winget packages: %w", err)
	}
	candidates := []string{}
	for _, folder := range folders {
		if candidate := filepath.Join(folder, fileName); isFile(candidate) {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("%w: %s", services.ErrFFmpegMissing, name)
	}
	return slices.MaxFunc(candidates, compareModTime), nil
}

func compareModTime(left, right string) int {
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	if leftErr != nil || rightErr != nil {
		return 0
	}
	return leftInfo.ModTime().Compare(rightInfo.ModTime())
}

func executableFileName(name string) string {
	if runtime.GOOS == "windows" {
		return name + windowsExecutable
	}
	return name
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func FFmpegForDiagnostics(configured string, lookPath func(string) (string, error), localAppData string) string {
	path, err := LocateFFmpeg(configured, lookPath, localAppData)
	if err != nil {
		return executableFileName(ffmpegName)
	}
	return path
}
