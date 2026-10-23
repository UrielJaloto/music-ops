package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/UrielJaloto/music-ops/services"
)

const (
	hiddenNamePrefix = "."
	workFolderName   = ".music-ops"
)

var audioExtensions = []string{".flac", ".wav", ".aiff", ".aif", ".ape", ".wv", ".alac", ".m4a", ".mp4", ".mp3", ".ogg", ".opus", ".wma"}

var skippedFolderNames = []string{workFolderName, "$recycle.bin", "system volume information"}

type FileScanner struct{}

func NewFileScanner() FileScanner {
	return FileScanner{}
}

func (scanner FileScanner) FindAudioFiles(ctx context.Context, folder string) (services.FoundFiles, error) {
	if err := requireFolder(folder); err != nil {
		return services.FoundFiles{}, err
	}
	found := services.FoundFiles{Audio: []services.FoundFile{}, Ignored: []string{}}
	err := filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return collectEntry(&found, folder, path, entry)
	})
	if err != nil {
		return services.FoundFiles{}, fmt.Errorf("scanning %s: %w", folder, err)
	}
	slices.SortFunc(found.Audio, func(left, right services.FoundFile) int {
		return strings.Compare(left.RelativePath, right.RelativePath)
	})
	slices.Sort(found.Ignored)
	return found, nil
}

func requireFolder(folder string) error {
	info, err := os.Stat(folder)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.IsDir()) {
		return fmt.Errorf("%w: %s", services.ErrFolderMissing, folder)
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", folder, err)
	}
	return nil
}

func collectEntry(found *services.FoundFiles, root, path string, entry fs.DirEntry) error {
	if entry.IsDir() {
		return skipDirectory(root, path, entry)
	}
	if !entry.Type().IsRegular() {
		return nil
	}
	relative := relativeSlashPath(root, path)
	if !isAudioFile(entry.Name()) {
		found.Ignored = append(found.Ignored, relative)
		return nil
	}
	info, err := entry.Info()
	if err != nil {
		return err
	}
	found.Audio = append(found.Audio, services.FoundFile{
		Path:         path,
		RelativePath: relative,
		Directory:    norm.NFC.String(filepath.Dir(path)),
		Size:         info.Size(),
		ModifiedAt:   info.ModTime(),
	})
	return nil
}

func skipDirectory(root, path string, entry fs.DirEntry) error {
	if path == root {
		return nil
	}
	if isSkippedFolder(entry) {
		return filepath.SkipDir
	}
	return nil
}

func isSkippedFolder(entry fs.DirEntry) bool {
	name := entry.Name()
	if strings.HasPrefix(name, hiddenNamePrefix) || slices.Contains(skippedFolderNames, strings.ToLower(name)) {
		return true
	}
	return isHiddenEntry(entry)
}

func isAudioFile(name string) bool {
	return slices.Contains(audioExtensions, strings.ToLower(filepath.Ext(name)))
}

func relativeSlashPath(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		relative = path
	}
	return norm.NFC.String(filepath.ToSlash(relative))
}
