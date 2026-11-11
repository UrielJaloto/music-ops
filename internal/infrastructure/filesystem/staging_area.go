package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/UrielJaloto/music-ops/internal/domain/runs"
	"github.com/UrielJaloto/music-ops/internal/domain/tracks"
	"github.com/UrielJaloto/music-ops/internal/services/importing"
)

const (
	stagingFolderName       = "staging"
	stagingProbeName        = ".write-probe"
	shortSourceLimit        = 240
	staleStagingAge         = 7 * 24 * time.Hour
	stagingFolderPermission = 0o750
	stagingFilePermission   = 0o600
)

type StagingArea struct {
	workRoot string
	link     func(source, destination string) error
	mutex    sync.RWMutex
	root     string
}

func NewStagingArea(workRoot string) *StagingArea {
	return &StagingArea{workRoot: workRoot, link: os.Link}
}

func (area *StagingArea) Prepare(id runs.RunID) error {
	root := filepath.Join(area.workRoot, stagingFolderName, string(id))
	if err := os.MkdirAll(root, stagingFolderPermission); err != nil {
		return fmt.Errorf("creating staging area: %w", err)
	}
	probe := filepath.Join(root, stagingProbeName)
	if err := errors.Join(os.WriteFile(probe, nil, stagingFilePermission), os.Remove(probe)); err != nil {
		return fmt.Errorf("work folder is not writable: %w", err)
	}
	area.mutex.Lock()
	defer area.mutex.Unlock()
	area.root = root
	return nil
}

func (area *StagingArea) BuildStagedPath(id tracks.TrackID, suffix string) string {
	return filepath.Join(area.readRoot(), string(id)+suffix)
}

func (area *StagingArea) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (area *StagingArea) ShortenSourcePath(ctx context.Context, source string) (string, error) {
	if len(source) <= shortSourceLimit {
		return source, nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	root, err := area.requireRoot()
	if err != nil {
		return "", err
	}
	short := filepath.Join(root, string(tracks.NewTrackID(source, 0, time.Time{}))+filepath.Ext(source))
	if area.Exists(short) {
		return short, nil
	}
	if err := area.link(source, short); err == nil {
		return short, nil
	}
	if err := copyFile(source, short); err != nil {
		return "", fmt.Errorf("shortening source path: %w", err)
	}
	return short, nil
}

func (area *StagingArea) Cleanup(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root := area.readRoot()
	if root == "" {
		return nil
	}
	info, statErr := os.Stat(root)
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("removing staging area: %w", err)
	}
	area.forgetRoot()
	if statErr == nil {
		removeStaleRuns(filepath.Dir(root), info.ModTime().Add(-staleStagingAge))
	}
	return nil
}

func (area *StagingArea) RemoveOtherRuns() error {
	root, err := area.requireRoot()
	if err != nil {
		return err
	}
	parent := filepath.Dir(root)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return fmt.Errorf("reading staging folders: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == filepath.Base(root) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(parent, entry.Name())); err != nil {
			return fmt.Errorf("removing interrupted staging folder: %w", err)
		}
	}
	return nil
}

func (area *StagingArea) readRoot() string {
	area.mutex.RLock()
	defer area.mutex.RUnlock()
	return area.root
}

func (area *StagingArea) requireRoot() (string, error) {
	root := area.readRoot()
	if root == "" {
		return "", importing.ErrWorkspaceClosed
	}
	return root, nil
}

func (area *StagingArea) forgetRoot() {
	area.mutex.Lock()
	defer area.mutex.Unlock()
	area.root = ""
}

func removeStaleRuns(parent string, cutoff time.Time) {
	entries, _ := os.ReadDir(parent)
	for _, entry := range entries {
		info, err := entry.Info()
		if err == nil && entry.IsDir() && info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(filepath.Join(parent, entry.Name()))
		}
	}
}

func copyFile(source, destination string) error {
	input, err := os.Open(filepath.Clean(source))
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	output, err := os.OpenFile(filepath.Clean(destination), os.O_CREATE|os.O_EXCL|os.O_WRONLY, stagingFilePermission)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		return errors.Join(err, output.Close())
	}
	return errors.Join(output.Sync(), output.Close())
}
