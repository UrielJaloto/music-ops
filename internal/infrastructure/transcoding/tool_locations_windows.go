//go:build windows

package transcoding

import (
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/sys/windows"
)

const (
	executableSuffix = ".exe"
	wingetLinks      = `Microsoft\WinGet\Links`
	wingetPackages   = `Microsoft\WinGet\Packages`
	wingetFFmpegBins = `Gyan.FFmpeg*\*\bin`
	freacFDKLibrary  = `freac\codecs\FDK-AAC.dll`
)

func listInstalledExecutables(fileName string) []string {
	localAppData, err := os.UserCacheDir()
	if err != nil {
		return nil
	}
	linked := filepath.Join(localAppData, wingetLinks, fileName)
	return append([]string{linked}, listPackagedExecutables(localAppData, fileName)...)
}

func listPackagedExecutables(localAppData, fileName string) []string {
	folders, err := filepath.Glob(filepath.Join(localAppData, wingetPackages, wingetFFmpegBins))
	if err != nil {
		return nil
	}
	candidates := []string{}
	for _, folder := range folders {
		if candidate := filepath.Join(folder, fileName); isFile(candidate) {
			candidates = append(candidates, candidate)
		}
	}
	slices.SortStableFunc(candidates, compareNewestFirst)
	return candidates
}

func compareNewestFirst(left, right string) int {
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	if leftErr != nil || rightErr != nil {
		return 0
	}
	return rightInfo.ModTime().Compare(leftInfo.ModTime())
}

func listFDKLibraryCandidates() []string {
	programFiles, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, 0)
	if err != nil {
		return nil
	}
	return []string{filepath.Join(programFiles, freacFDKLibrary)}
}
