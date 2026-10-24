package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrDestinationExists = errors.New("destination already exists")

type FileMover struct {
	rename func(source, destination string) error
}

func NewFileMover() FileMover {
	return FileMover{rename: os.Rename}
}

func (mover FileMover) Move(ctx context.Context, source, destination string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	target := filepath.FromSlash(destination)
	if mover.Exists(target) {
		return fmt.Errorf("%w: %s", ErrDestinationExists, target)
	}
	if err := os.MkdirAll(filepath.Dir(target), stagingFolderPermission); err != nil {
		return fmt.Errorf("creating destination folder: %w", err)
	}
	err := mover.rename(source, target)
	if isCrossDevice(err) {
		return moveAcrossDevices(source, target)
	}
	if err != nil {
		return fmt.Errorf("moving %s: %w", source, err)
	}
	return nil
}

func (mover FileMover) Remove(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(filepath.FromSlash(path)); err != nil {
		return fmt.Errorf("removing %s: %w", path, err)
	}
	return nil
}

func (mover FileMover) Exists(path string) bool {
	_, err := os.Stat(filepath.FromSlash(path))
	return err == nil
}

func isCrossDevice(err error) bool {
	return errors.Is(err, crossDeviceErrno)
}

func moveAcrossDevices(source, target string) error {
	if err := copyFile(source, target); err != nil {
		return fmt.Errorf("copying %s across devices: %w", source, err)
	}
	if err := os.Remove(source); err != nil {
		return fmt.Errorf("removing moved source %s: %w", source, err)
	}
	return nil
}
