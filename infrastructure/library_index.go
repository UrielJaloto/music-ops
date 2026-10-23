package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
)

const libraryExtension = ".m4a"

type LibraryIndex struct{}

func NewLibraryIndex() LibraryIndex {
	return LibraryIndex{}
}

func (index LibraryIndex) ListTrackPaths(ctx context.Context, root string) ([]string, error) {
	paths := []string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if entry.IsDir() && entry.Name() == workFolderName {
			return filepath.SkipDir
		}
		if isLibraryTrack(entry) {
			paths = append(paths, relativeSlashPath(root, path))
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing library %s: %w", root, err)
	}
	slices.Sort(paths)
	return paths, nil
}

func isLibraryTrack(entry fs.DirEntry) bool {
	return entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(entry.Name()), libraryExtension)
}
