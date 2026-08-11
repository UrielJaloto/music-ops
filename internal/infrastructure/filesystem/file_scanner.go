package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/UrielJaloto/music-ops/internal/services/discovery"
	"golang.org/x/text/unicode/norm"
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

func (scanner FileScanner) FindAudioFiles(ctx context.Context, folder string) (discovery.FoundFiles, error) {
	if err := requireFolder(folder); err != nil {
		return discovery.FoundFiles{}, err
	}
	found := discovery.FoundFiles{Audio: []discovery.FoundFile{}, Ignored: []string{}}
	err := filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
		if err := errors.Join(err, ctx.Err()); err != nil {
			return err
		}
		return collectEntry(&found, folder, path, entry)
	})
	if err != nil {
		return discovery.FoundFiles{}, fmt.Errorf("scanning %s: %w", folder, err)
	}
	slices.SortFunc(found.Audio, func(left, right discovery.FoundFile) int {
		return strings.Compare(left.RelativePath, right.RelativePath)
	})
	slices.Sort(found.Ignored)
	return found, nil
}

func requireFolder(folder string) error {
	info, err := os.Stat(folder)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.IsDir()) {
		return fmt.Errorf("%w: %s", discovery.ErrFolderMissing, folder)
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", folder, err)
	}
	return nil
}

func collectEntry(found *discovery.FoundFiles, root, path string, entry fs.DirEntry) error {
	if entry.IsDir() {
		return skipDirectory(root, path, entry)
	}
	if !entry.Type().IsRegular() {
		return nil
	}
	relative := toRelativeSlashPath(root, path)
	if !isAudioFile(entry.Name()) {
		found.Ignored = append(found.Ignored, relative)
		return nil
	}
	info, err := entry.Info()
	if err == nil {
		found.Audio = append(found.Audio, discovery.FoundFile{
			Path:         path,
			RelativePath: relative,
			Directory:    norm.NFC.String(filepath.Dir(path)),
			Size:         info.Size(),
			ModifiedAt:   info.ModTime(),
		})
	}
	return err
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

func toRelativeSlashPath(root, path string) string {
	relative := strings.TrimPrefix(strings.TrimPrefix(path, root), string(filepath.Separator))
	return norm.NFC.String(filepath.ToSlash(relative))
}
