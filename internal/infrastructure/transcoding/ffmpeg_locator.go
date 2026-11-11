package transcoding

import (
	"fmt"
	"os"

	"github.com/UrielJaloto/music-ops/internal/services/diagnostics"
)

const (
	ffmpegName  = "ffmpeg"
	ffprobeName = "ffprobe"
)

func LocateFFmpeg(configured string, lookPath func(string) (string, error)) (string, error) {
	return locateExecutable(ffmpegName, configured, lookPath)
}

func LocateFFprobe(configured string, lookPath func(string) (string, error)) (string, error) {
	return locateExecutable(ffprobeName, configured, lookPath)
}

func LocateFDKLibrary(configured string) string {
	if configured != "" {
		return configured
	}
	return findFirstFile(listFDKLibraryCandidates())
}

func locateExecutable(name, configured string, lookPath func(string) (string, error)) (string, error) {
	if configured != "" {
		return findExistingFile(configured, name)
	}
	if path, err := lookPath(name); err == nil {
		return path, nil
	}
	if path := findFirstFile(listInstalledExecutables(buildExecutableFileName(name))); path != "" {
		return path, nil
	}
	return "", fmt.Errorf("%w: %s", diagnostics.ErrFFmpegMissing, name)
}

func findExistingFile(path, name string) (string, error) {
	if !isFile(path) {
		return "", fmt.Errorf("%w: %s not found at %s", diagnostics.ErrFFmpegMissing, name, path)
	}
	return path, nil
}

func findFirstFile(candidates []string) string {
	for _, candidate := range candidates {
		if isFile(candidate) {
			return candidate
		}
	}
	return ""
}

func buildExecutableFileName(name string) string {
	return name + executableSuffix
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
