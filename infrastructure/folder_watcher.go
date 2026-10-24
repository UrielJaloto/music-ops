package infrastructure

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/UrielJaloto/music-ops/services"
)

const (
	looseFolderPrefix = "_loose-"
	looseFolderLayout = "20060102-150405"
	looseQuietPeriod  = 30 * time.Second
)

type WatchClock interface {
	Now() time.Time
	After(duration time.Duration) <-chan time.Time
}

type FolderWatcher struct {
	clock      WatchClock
	looseQuiet time.Duration
}

func NewFolderWatcher(clock WatchClock) FolderWatcher {
	return FolderWatcher{clock: clock, looseQuiet: looseQuietPeriod}
}

func (watcher FolderWatcher) Existing(ctx context.Context, inbox string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(inbox)
	if err != nil {
		return nil, fmt.Errorf("reading inbox %s: %w", inbox, err)
	}
	folders := []string{}
	for _, entry := range entries {
		if entry.IsDir() && !isIgnoredInboxName(entry.Name()) {
			folders = append(folders, filepath.Join(inbox, entry.Name()))
		}
	}
	slices.Sort(folders)
	return folders, nil
}

func (watcher FolderWatcher) Arrivals(ctx context.Context, inbox string) (<-chan string, error) {
	notifier, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("starting inbox watcher: %w", err)
	}
	if err := notifier.Add(inbox); err != nil {
		return nil, closeAfterFailure(fmt.Errorf("watching inbox %s: %w", inbox, err), notifier)
	}
	existing, err := watcher.Existing(ctx, inbox)
	if err != nil {
		return nil, closeAfterFailure(err, notifier)
	}
	loop := arrivalLoop{watcher: watcher, inbox: inbox, notifier: notifier, output: make(chan string), seen: map[string]bool{}}
	for _, folder := range existing {
		loop.seen[folder] = true
	}
	go loop.run(ctx)
	return loop.output, nil
}

func closeAfterFailure(err error, notifier *fsnotify.Watcher) error {
	if closeErr := notifier.Close(); closeErr != nil {
		return fmt.Errorf("%w (closing watcher: %w)", err, closeErr)
	}
	return err
}

type arrivalLoop struct {
	watcher    FolderWatcher
	inbox      string
	notifier   *fsnotify.Watcher
	output     chan string
	seen       map[string]bool
	looseTimer <-chan time.Time
}

func (loop *arrivalLoop) run(ctx context.Context) {
	defer close(loop.output)
	defer loop.notifier.Close()
	if hasLooseAudio(loop.inbox) {
		loop.looseTimer = loop.watcher.clock.After(loop.watcher.looseQuiet)
	}
	isRunning := true
	for isRunning {
		isRunning = loop.step(ctx)
	}
}

func (loop *arrivalLoop) step(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case event, isOpen := <-loop.notifier.Events:
		return isOpen && loop.handle(ctx, event)
	case _, isOpen := <-loop.notifier.Errors:
		return isOpen
	case <-loop.looseTimer:
		loop.looseTimer = nil
		return loop.groupLooseFiles(ctx)
	}
}

func (loop *arrivalLoop) handle(ctx context.Context, event fsnotify.Event) bool {
	if !event.Has(fsnotify.Create) || isIgnoredInboxName(filepath.Base(event.Name)) {
		return true
	}
	info, err := os.Stat(event.Name)
	if err != nil {
		return true
	}
	if info.IsDir() {
		return loop.emit(ctx, event.Name)
	}
	if isAudioFile(event.Name) {
		loop.looseTimer = loop.watcher.clock.After(loop.watcher.looseQuiet)
	}
	return true
}

func (loop *arrivalLoop) groupLooseFiles(ctx context.Context) bool {
	folder := filepath.Join(loop.inbox, looseFolderPrefix+loop.watcher.clock.Now().Format(looseFolderLayout))
	isMoved, err := moveLooseAudio(loop.inbox, folder)
	if err != nil || !isMoved {
		return true
	}
	return loop.emit(ctx, folder)
}

func (loop *arrivalLoop) emit(ctx context.Context, folder string) bool {
	if loop.seen[folder] {
		return true
	}
	loop.seen[folder] = true
	select {
	case loop.output <- folder:
		return true
	case <-ctx.Done():
		return false
	}
}

func hasLooseAudio(inbox string) bool {
	return len(looseAudioNames(inbox)) > 0
}

func looseAudioNames(inbox string) []string {
	entries, err := os.ReadDir(inbox)
	if err != nil {
		return []string{}
	}
	names := []string{}
	for _, entry := range entries {
		if entry.Type().IsRegular() && isAudioFile(entry.Name()) && !isIgnoredInboxName(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	return names
}

func moveLooseAudio(inbox, folder string) (bool, error) {
	names := looseAudioNames(inbox)
	if len(names) == 0 {
		return false, nil
	}
	if err := os.MkdirAll(folder, stagingFolderPermission); err != nil {
		return false, fmt.Errorf("creating loose folder: %w", err)
	}
	for _, name := range names {
		if err := os.Rename(filepath.Join(inbox, name), filepath.Join(folder, name)); err != nil {
			return false, fmt.Errorf("moving loose file %s: %w", name, err)
		}
	}
	return true, nil
}

func isIgnoredInboxName(name string) bool {
	if strings.HasPrefix(name, hiddenNamePrefix) {
		return true
	}
	return slices.Contains([]string{services.ProcessedFolderName, services.FailedFolderName, workFolderName}, name)
}
