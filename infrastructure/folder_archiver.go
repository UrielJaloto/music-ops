package infrastructure

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

const firstCollisionNumber = 2

type FolderArchiver struct {
	rename func(source, destination string) error
}

func NewFolderArchiver() FolderArchiver {
	return FolderArchiver{rename: os.Rename}
}

func (archiver FolderArchiver) Archive(ctx context.Context, folder, destination string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	parent := filepath.FromSlash(destination)
	if err := os.MkdirAll(parent, stagingFolderPermission); err != nil {
		return fmt.Errorf("creating archive folder: %w", err)
	}
	target := freeArchiveName(filepath.Join(parent, filepath.Base(folder)))
	err := archiver.rename(folder, target)
	if isCrossDevice(err) {
		return copyFolderAcrossDevices(folder, target)
	}
	if err != nil {
		return fmt.Errorf("archiving %s: %w", folder, err)
	}
	return nil
}

func freeArchiveName(target string) string {
	candidate := target
	for number := firstCollisionNumber; pathExists(candidate); number++ {
		candidate = target + " (" + strconv.Itoa(number) + ")"
	}
	return candidate
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyFolderAcrossDevices(folder, target string) error {
	err := filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(folder, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, stagingFolderPermission)
		}
		return copyFile(path, destination)
	})
	if err != nil {
		return fmt.Errorf("copying %s across devices: %w", folder, err)
	}
	if err := os.RemoveAll(folder); err != nil {
		return fmt.Errorf("removing archived source %s: %w", folder, err)
	}
	return nil
}
