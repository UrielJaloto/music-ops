package services

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
)

const (
	ProcessedFolderName = "_processed"
	FailedFolderName    = "_failed"
	watchPathSeparator  = "/"
)

type FolderWatcher interface {
	Existing(ctx context.Context, inbox string) ([]string, error)
	Arrivals(ctx context.Context, inbox string) (<-chan string, error)
}

type FolderImporter interface {
	Import(ctx context.Context, input ImportInput) (ImportOutput, error)
}

type FolderArchiver interface {
	Archive(ctx context.Context, folder, destination string) error
}

type FolderStability interface {
	IsStable(ctx context.Context, folder string, quietPeriod time.Duration) (bool, error)
}

type WatchProgress interface {
	FolderImported(folder string, exitStatus int)
	FolderFailed(folder, reason string)
}

type WatchInput struct {
	Inbox       string
	Import      ImportInput
	QuietPeriod time.Duration
}

type WatchService struct {
	watcher   FolderWatcher
	importer  FolderImporter
	archiver  FolderArchiver
	stability FolderStability
	progress  WatchProgress
}

func NewWatchService(watcher FolderWatcher, importer FolderImporter, archiver FolderArchiver, stability FolderStability, progress WatchProgress) WatchService {
	return WatchService{watcher: watcher, importer: importer, archiver: archiver, stability: stability, progress: progress}
}

func (service WatchService) Watch(ctx context.Context, input WatchInput) error {
	if strings.TrimSpace(input.Inbox) == "" {
		return ErrFolderMissing
	}
	existing, err := service.watcher.Existing(ctx, input.Inbox)
	if err != nil {
		return fmt.Errorf("listing inbox %s: %w", input.Inbox, err)
	}
	arrivals, err := service.watcher.Arrivals(ctx, input.Inbox)
	if err != nil {
		return fmt.Errorf("watching inbox %s: %w", input.Inbox, err)
	}
	for _, folder := range existing {
		if err := service.process(ctx, input, folder); err != nil {
			return ignoreCancellation(err)
		}
	}
	return ignoreCancellation(service.processArrivals(ctx, input, arrivals))
}

func (service WatchService) processArrivals(ctx context.Context, input WatchInput, arrivals <-chan string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case folder, isOpen := <-arrivals:
			if !isOpen {
				return nil
			}
			if err := service.process(ctx, input, folder); err != nil {
				return err
			}
		}
	}
}

func (service WatchService) process(ctx context.Context, input WatchInput, folder string) error {
	if isArchiveFolder(folder) {
		return nil
	}
	if err := service.waitUntilStable(ctx, folder, input.QuietPeriod); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		service.progress.FolderFailed(folder, err.Error())
		return nil
	}
	importInput := input.Import
	importInput.Folder = folder
	output, err := service.importer.Import(ctx, importInput)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	category := FailedFolderName
	if err != nil {
		service.progress.FolderFailed(folder, err.Error())
	} else {
		service.progress.FolderImported(folder, output.ExitStatus)
		category = categoryFor(output.ExitStatus)
	}
	service.archive(ctx, folder, strings.TrimRight(input.Inbox, "/\\")+watchPathSeparator+category)
	return nil
}

func (service WatchService) waitUntilStable(ctx context.Context, folder string, quietPeriod time.Duration) error {
	for {
		isStable, err := service.stability.IsStable(ctx, folder, quietPeriod)
		if err != nil {
			return fmt.Errorf("checking stability of %s: %w", folder, err)
		}
		if isStable {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

func (service WatchService) archive(ctx context.Context, folder, destination string) {
	if err := service.archiver.Archive(ctx, folder, destination); err != nil {
		service.progress.FolderFailed(folder, fmt.Sprintf("archiving: %v", err))
	}
}

func categoryFor(exitStatus int) string {
	if exitStatus == 0 {
		return ProcessedFolderName
	}
	return FailedFolderName
}

func isArchiveFolder(folder string) bool {
	name := path.Base(strings.ReplaceAll(folder, "\\", watchPathSeparator))
	return name == ProcessedFolderName || name == FailedFolderName
}

func ignoreCancellation(err error) error {
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
