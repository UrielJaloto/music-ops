package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/UrielJaloto/music-ops/domain"
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

func PrepareStagingArea(output string, id domain.RunID) (StagingArea, error) {
	root := filepath.Join(output, workFolderName, stagingFolderName, string(id))
	if err := os.MkdirAll(root, stagingFolderPermission); err != nil {
		return StagingArea{}, fmt.Errorf("creating staging area: %w", err)
	}
	probe := filepath.Join(root, stagingProbeName)
	if err := os.WriteFile(probe, nil, stagingFilePermission); err != nil {
		return StagingArea{}, fmt.Errorf("output folder is not writable: %w", err)
	}
	if err := os.Remove(probe); err != nil {
		return StagingArea{}, fmt.Errorf("output folder is not writable: %w", err)
	}
	return StagingArea{root: root, link: os.Link}, nil
}

func (area StagingArea) StagedPath(id domain.TrackID, suffix string) string {
	return filepath.Join(area.root, string(id)+suffix)
}

func (area StagingArea) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (area StagingArea) ShortSourcePath(ctx context.Context, source string) (string, error) {
	if len(source) <= shortSourceLimit {
		return source, nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	short := filepath.Join(area.root, string(domain.NewTrackID(source, 0, time.Time{}))+filepath.Ext(source))
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
	info, err := os.Stat(area.root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading staging area: %w", err)
	}
	if err := os.RemoveAll(area.root); err != nil {
		return fmt.Errorf("removing staging area: %w", err)
	}
	return removeStaleRuns(filepath.Dir(area.root), info.ModTime().Add(-staleStagingAge))
}

func removeStaleRuns(parent string, cutoff time.Time) error {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return fmt.Errorf("reading staging folders: %w", err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !entry.IsDir() || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(parent, entry.Name())); err != nil {
			return fmt.Errorf("removing stale staging folder: %w", err)
		}
	}
	return nil
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
	if err := output.Sync(); err != nil {
		return errors.Join(err, output.Close())
	}
	return output.Close()
}
