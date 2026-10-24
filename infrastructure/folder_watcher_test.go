//go:build integration

package infrastructure

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

const arrivalTimeout = 5 * time.Second

type controlledClock struct {
	mutex sync.Mutex
	now   time.Time
	fires []chan time.Time
}

func newControlledClock() *controlledClock {
	return &controlledClock{now: time.Date(2026, 10, 16, 8, 30, 5, 0, time.UTC)}
}

func (clock *controlledClock) Now() time.Time { return clock.now }

func (clock *controlledClock) After(time.Duration) <-chan time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	fire := make(chan time.Time, 1)
	clock.fires = append(clock.fires, fire)
	return fire
}

func (clock *controlledClock) firePending() {
	clock.mutex.Lock()
	pending := clock.fires
	clock.fires = nil
	clock.mutex.Unlock()
	for _, fire := range pending {
		fire <- clock.now
	}
}

func (clock *controlledClock) fireUntilArrival(t *testing.T, arrivals <-chan string) string {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(arrivalTimeout)
	for {
		select {
		case folder := <-arrivals:
			return folder
		case <-ticker.C:
			clock.firePending()
		case <-deadline:
			t.Fatal("no arrival")
			return ""
		}
	}
}

func makeFolders(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.MkdirAll(filepath.Join(root, name), 0o750); err != nil {
			t.Fatal(err)
		}
	}
}

func receiveArrival(t *testing.T, arrivals <-chan string) string {
	t.Helper()
	select {
	case folder := <-arrivals:
		return folder
	case <-time.After(arrivalTimeout):
		t.Fatal("no arrival")
		return ""
	}
}

func startArrivals(t *testing.T, watcher FolderWatcher, inbox string) <-chan string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	arrivals, err := watcher.Arrivals(ctx, inbox)
	if err != nil {
		t.Fatal(err)
	}
	return arrivals
}

func TestFolderWatcher_Existing_SkipsIgnoredNames(t *testing.T) {
	inbox := t.TempDir()
	makeFolders(t, inbox, "b-album", "a-album", "_processed", "_failed", ".music-ops", ".hidden")
	writeTestFile(t, filepath.Join(inbox, "loose.mp3"), "x")

	folders, err := NewFolderWatcher(newControlledClock()).Existing(context.Background(), inbox)

	expected := []string{filepath.Join(inbox, "a-album"), filepath.Join(inbox, "b-album")}
	if diff := cmp.Diff(expected, folders); diff != "" || err != nil {
		t.Fatalf("folders (-want +got):\n%s err = %v", diff, err)
	}
}

func TestFolderWatcher_Existing_MissingInbox(t *testing.T) {
	_, err := NewFolderWatcher(newControlledClock()).Existing(context.Background(), filepath.Join(t.TempDir(), "missing"))

	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v", err)
	}
}

func TestFolderWatcher_Arrivals_NewFolderEmittedOnceAndIgnoredNamesSkipped(t *testing.T) {
	inbox := t.TempDir()
	makeFolders(t, inbox, "existing")
	arrivals := startArrivals(t, NewFolderWatcher(newControlledClock()), inbox)

	makeFolders(t, inbox, "_processed", ".hidden", "first")
	first := receiveArrival(t, arrivals)
	writeTestFile(t, filepath.Join(inbox, "first", "song.flac"), "x")
	makeFolders(t, inbox, "second")
	second := receiveArrival(t, arrivals)

	if first != filepath.Join(inbox, "first") || second != filepath.Join(inbox, "second") {
		t.Fatalf("arrivals = %s, %s", first, second)
	}
}

func TestFolderWatcher_Arrivals_GroupsLooseFiles(t *testing.T) {
	inbox := t.TempDir()
	clock := newControlledClock()
	arrivals := startArrivals(t, NewFolderWatcher(clock), inbox)

	writeTestFile(t, filepath.Join(inbox, "one.mp3"), "1")
	folder := clock.fireUntilArrival(t, arrivals)

	expected := filepath.Join(inbox, "_loose-20261016-083005")
	if folder != expected || !isFile(filepath.Join(expected, "one.mp3")) || pathExists(filepath.Join(inbox, "one.mp3")) {
		t.Fatalf("folder = %s", folder)
	}
}

func TestFolderWatcher_Arrivals_GroupsLooseFilesPresentAtStart(t *testing.T) {
	inbox := t.TempDir()
	writeTestFile(t, filepath.Join(inbox, "early.flac"), "1")
	writeTestFile(t, filepath.Join(inbox, "notes.txt"), "1")
	clock := newControlledClock()
	arrivals := startArrivals(t, NewFolderWatcher(clock), inbox)

	folder := clock.fireUntilArrival(t, arrivals)

	if !isFile(filepath.Join(folder, "early.flac")) || !isFile(filepath.Join(inbox, "notes.txt")) {
		t.Fatalf("folder = %s", folder)
	}
}

func TestFolderWatcher_Arrivals_ClosesWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	arrivals, err := NewFolderWatcher(newControlledClock()).Arrivals(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	cancel()

	select {
	case _, isOpen := <-arrivals:
		if isOpen {
			t.Fatal("unexpected arrival")
		}
	case <-time.After(arrivalTimeout):
		t.Fatal("channel not closed")
	}
}

func TestFolderWatcher_Arrivals_MissingInbox(t *testing.T) {
	_, err := NewFolderWatcher(newControlledClock()).Arrivals(context.Background(), filepath.Join(t.TempDir(), "missing"))

	if err == nil {
		t.Fatal("missing inbox accepted")
	}
}

type hookClock struct {
	beforeFire func()
}

func (clock hookClock) After(time.Duration) <-chan time.Time {
	clock.beforeFire()
	fire := make(chan time.Time, 1)
	fire <- time.Time{}
	return fire
}

func TestFolderStability_IsStable(t *testing.T) {
	cases := []struct {
		name     string
		change   func(t *testing.T, folder string)
		expected bool
	}{
		{name: "Unchanged_IsStable", change: func(*testing.T, string) {}, expected: true},
		{name: "FileGrows_IsNotStable", change: func(t *testing.T, folder string) {
			writeTestFile(t, filepath.Join(folder, "a.flac"), "longer content")
		}, expected: false},
		{name: "FileAdded_IsNotStable", change: func(t *testing.T, folder string) {
			writeTestFile(t, filepath.Join(folder, "sub", "b.flac"), "x")
		}, expected: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			folder := t.TempDir()
			writeTestFile(t, filepath.Join(folder, "a.flac"), "x")
			stability := NewFolderStability(hookClock{beforeFire: func() { testCase.change(t, folder) }})

			isStable, err := stability.IsStable(context.Background(), folder, 20*time.Second)

			if err != nil || isStable != testCase.expected {
				t.Fatalf("stable = %v err = %v", isStable, err)
			}
		})
	}
}

func TestFolderStability_Errors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	_, missingErr := NewFolderStability(hookClock{beforeFire: func() {}}).IsStable(context.Background(), missing, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	folder := t.TempDir()
	cancel()
	_, cancelErr := NewFolderStability(newControlledClock()).IsStable(ctx, folder, time.Second)
	removed := t.TempDir()
	_, removedErr := NewFolderStability(hookClock{beforeFire: func() { _ = os.RemoveAll(removed) }}).IsStable(context.Background(), removed, time.Second)

	if !errors.Is(missingErr, os.ErrNotExist) || !errors.Is(cancelErr, context.Canceled) || !errors.Is(removedErr, os.ErrNotExist) {
		t.Fatalf("errors = %v %v %v", missingErr, cancelErr, removedErr)
	}
}

func TestFolderArchiver_Archive_NameCollisions(t *testing.T) {
	inbox := t.TempDir()
	destination := filepath.Join(inbox, "_processed")
	archiver := NewFolderArchiver()
	var archived []string

	for range 3 {
		writeTestFile(t, filepath.Join(inbox, "Album", "song.flac"), "x")
		if err := archiver.Archive(context.Background(), filepath.Join(inbox, "Album"), filepath.ToSlash(destination)); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		archived = append(archived, entry.Name())
	}

	if diff := cmp.Diff([]string{"Album", "Album (2)", "Album (3)"}, archived); diff != "" || pathExists(filepath.Join(inbox, "Album")) {
		t.Fatalf("archived (-want +got):\n%s", diff)
	}
}

func TestFolderArchiver_Archive_CrossDevice(t *testing.T) {
	inbox := t.TempDir()
	writeTestFile(t, filepath.Join(inbox, "Album", "disc 1", "song.flac"), "audio")
	archiver := FolderArchiver{rename: func(oldPath, newPath string) error {
		return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: crossDeviceErrno}
	}}

	err := archiver.Archive(context.Background(), filepath.Join(inbox, "Album"), filepath.Join(inbox, "_failed"))

	content, readErr := os.ReadFile(filepath.Join(inbox, "_failed", "Album", "disc 1", "song.flac"))
	if err != nil || readErr != nil || string(content) != "audio" || pathExists(filepath.Join(inbox, "Album")) {
		t.Fatalf("content = %q err = %v %v", content, err, readErr)
	}
}

func TestFolderArchiver_Archive_Errors(t *testing.T) {
	errRename := errors.New("in use")
	inbox := t.TempDir()
	makeFolders(t, inbox, "Album")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	renameErr := FolderArchiver{rename: func(string, string) error { return errRename }}.Archive(context.Background(), filepath.Join(inbox, "Album"), filepath.Join(inbox, "_failed"))
	cancelErr := NewFolderArchiver().Archive(ctx, filepath.Join(inbox, "Album"), filepath.Join(inbox, "_failed"))

	if !errors.Is(renameErr, errRename) || !errors.Is(cancelErr, context.Canceled) {
		t.Fatalf("errors = %v %v", renameErr, cancelErr)
	}
}
