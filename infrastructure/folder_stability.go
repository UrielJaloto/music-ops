package infrastructure

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"time"
)

type StabilityClock interface {
	After(duration time.Duration) <-chan time.Time
}

type FolderStability struct {
	clock StabilityClock
}

func NewFolderStability(clock StabilityClock) FolderStability {
	return FolderStability{clock: clock}
}

type fileState struct {
	size       int64
	modifiedAt time.Time
}

func (stability FolderStability) IsStable(ctx context.Context, folder string, quietPeriod time.Duration) (bool, error) {
	before, err := snapshotFolder(ctx, folder)
	if err != nil {
		return false, err
	}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-stability.clock.After(quietPeriod):
	}
	after, err := snapshotFolder(ctx, folder)
	if err != nil {
		return false, err
	}
	return maps.Equal(before, after), nil
}

func snapshotFolder(ctx context.Context, folder string) (map[string]fileState, error) {
	snapshot := map[string]fileState{}
	err := filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		snapshot[path] = fileState{size: info.Size(), modifiedAt: info.ModTime()}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading folder %s: %w", folder, err)
	}
	return snapshot, nil
}
