package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/UrielJaloto/music-ops/internal/domain/runs"
	"github.com/UrielJaloto/music-ops/internal/domain/tracks"
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
	root string
	link func(source, destination string) error
}

func PrepareStagingArea(workRoot string, id runs.RunID) (StagingArea, error) {
	root := filepath.Join(workRoot, stagingFolderName, string(id))
	if err := os.MkdirAll(root, stagingFolderPermission); err != nil {
		return StagingArea{}, fmt.Errorf("creating staging area: %w", err)
	}
	probe := filepath.Join(root, stagingProbeName)
	if err := errors.Join(os.WriteFile(probe, nil, stagingFilePermission), os.Remove(probe)); err != nil {
		return StagingArea{}, fmt.Errorf("work folder is not writable: %w", err)
	}
	return StagingArea{root: root, link: os.Link}, nil
}

func (area StagingArea) BuildStagedPath(id tracks.TrackID, suffix string) string {
	return filepath.Join(area.root, string(id)+suffix)
}

func (area StagingArea) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (area StagingArea) ShortenSourcePath(ctx context.Context, source string) (string, error) {
	if len(source) <= shortSourceLimit {
		return source, nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	short := filepath.Join(area.root, string(tracks.NewTrackID(source, 0, time.Time{}))+filepath.Ext(source))
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

func (area StagingArea) Cleanup(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, statErr := os.Stat(area.root)
	if err := os.RemoveAll(area.root); err != nil {
		return fmt.Errorf("removing staging area: %w", err)
	}
	if statErr == nil {
		removeStaleRuns(filepath.Dir(area.root), info.ModTime().Add(-staleStagingAge))
	}
	return nil
}

func (area StagingArea) RemoveOtherRuns() error {
	parent := filepath.Dir(area.root)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return fmt.Errorf("reading staging folders: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == filepath.Base(area.root) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(parent, entry.Name())); err != nil {
			return fmt.Errorf("removing interrupted staging folder: %w", err)
		}
	}
	return nil
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
