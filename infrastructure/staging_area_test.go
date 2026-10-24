//go:build integration

package infrastructure

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func prepareTestStaging(t *testing.T) (StagingArea, string) {
	t.Helper()
	output := t.TempDir()
	area, err := PrepareStagingArea(output, "20261016-083005-abcd")
	if err != nil {
		t.Fatal(err)
	}
	return area, output
}

func TestPrepareStagingArea_CreatesRunFolder(t *testing.T) {
	area, output := prepareTestStaging(t)

	expectedRoot := filepath.Join(output, ".music-ops", "staging", "20261016-083005-abcd")
	entries, err := os.ReadDir(expectedRoot)
	if err != nil || area.root != expectedRoot || len(entries) != 0 {
		t.Fatalf("root = %s entries = %v err = %v", area.root, entries, err)
	}
}

func TestPrepareStagingArea_OutputIsAFile(t *testing.T) {
	output := filepath.Join(t.TempDir(), "file")
	writeTestFile(t, output, "x")

	_, err := PrepareStagingArea(output, "20261016-083005-abcd")

	if err == nil || !strings.Contains(err.Error(), "creating staging area") {
		t.Fatalf("err = %v", err)
	}
}

func TestStagingArea_StagedPathAndExists(t *testing.T) {
	area, _ := prepareTestStaging(t)

	path := area.StagedPath("track1", ".audio.m4a")
	isMissingBefore := !area.Exists(path)
	writeTestFile(t, path, "x")

	if path != filepath.Join(area.root, "track1.audio.m4a") || !isMissingBefore || !area.Exists(path) {
		t.Fatalf("path = %s", path)
	}
}

func TestStagingArea_ShortSourcePath(t *testing.T) {
	area, _ := prepareTestStaging(t)
	folder := t.TempDir()
	short := filepath.Join(folder, "short.flac")
	long := filepath.Join(folder, strings.Repeat("a", 120), strings.Repeat("b", 120)+".flac")
	writeTestFile(t, short, "short")
	writeTestFile(t, long, "long")

	shortResult, shortErr := area.ShortSourcePath(context.Background(), short)
	longResult, longErr := area.ShortSourcePath(context.Background(), long)
	again, againErr := area.ShortSourcePath(context.Background(), long)

	if shortErr != nil || shortResult != short {
		t.Fatalf("short = %s err = %v", shortResult, shortErr)
	}
	content, err := os.ReadFile(longResult)
	if longErr != nil || againErr != nil || err != nil || string(content) != "long" || len(longResult) > shortSourceLimit || again != longResult || filepath.Dir(longResult) != area.root {
		t.Fatalf("long = %s content = %q err = %v %v %v", longResult, content, longErr, againErr, err)
	}
}

func TestStagingArea_ShortSourcePath_LinkFailsFallsBackToCopy(t *testing.T) {
	area, _ := prepareTestStaging(t)
	long := filepath.Join(t.TempDir(), strings.Repeat("a", 250)+".flac")
	writeTestFile(t, long, "copied")
	var linkCalls int
	area.link = func(string, string) error {
		linkCalls++
		return errors.New("links unsupported")
	}

	result, err := area.ShortSourcePath(context.Background(), long)

	content, readErr := os.ReadFile(result)
	if err != nil || readErr != nil || string(content) != "copied" || linkCalls != 1 {
		t.Fatalf("result = %s content = %q err = %v %v", result, content, err, readErr)
	}
}

func TestStagingArea_ShortSourcePath_MissingSource(t *testing.T) {
	area, _ := prepareTestStaging(t)
	missing := filepath.Join(t.TempDir(), strings.Repeat("m", 250)+".flac")

	_, err := area.ShortSourcePath(context.Background(), missing)

	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v", err)
	}
}

func TestStagingArea_ShortSourcePath_Cancelled(t *testing.T) {
	area, _ := prepareTestStaging(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := area.ShortSourcePath(ctx, strings.Repeat("x", 300))

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestStagingArea_Cleanup(t *testing.T) {
	area, output := prepareTestStaging(t)
	parent := filepath.Join(output, ".music-ops", "staging")
	writeTestFile(t, area.StagedPath("t1", ".m4a"), "x")
	recent := filepath.Join(parent, "recent-run")
	old := filepath.Join(parent, "old-run")
	writeTestFile(t, filepath.Join(recent, "a.m4a"), "x")
	writeTestFile(t, filepath.Join(old, "a.m4a"), "x")
	runTime := time.Now()
	setModTime(t, area.root, runTime)
	setModTime(t, recent, runTime.Add(-6*24*time.Hour))
	setModTime(t, old, runTime.Add(-8*24*time.Hour))

	err := area.Cleanup(context.Background())

	if err != nil || area.Exists(area.root) || !area.Exists(recent) || area.Exists(old) {
		t.Fatalf("err = %v root = %v recent = %v old = %v", err, area.Exists(area.root), area.Exists(recent), area.Exists(old))
	}
}

func TestStagingArea_Cleanup_AlreadyRemoved(t *testing.T) {
	area, _ := prepareTestStaging(t)
	if err := os.RemoveAll(area.root); err != nil {
		t.Fatal(err)
	}

	if err := area.Cleanup(context.Background()); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestStagingArea_Cleanup_Cancelled(t *testing.T) {
	area, _ := prepareTestStaging(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := area.Cleanup(ctx)

	if !errors.Is(err, context.Canceled) || !area.Exists(area.root) {
		t.Fatalf("err = %v", err)
	}
}

func setModTime(t *testing.T, path string, modifiedAt time.Time) {
	t.Helper()
	if err := os.Chtimes(path, modifiedAt, modifiedAt); err != nil {
		t.Fatal(err)
	}
}

func TestFileMover_Move(t *testing.T) {
	folder := t.TempDir()
	source := filepath.Join(folder, "staged.m4a")
	writeTestFile(t, source, "audio")
	destination := filepath.ToSlash(filepath.Join(folder, "library", "Album", strings.Repeat("n", 200)+".m4a"))

	err := NewFileMover().Move(context.Background(), source, destination)

	content, readErr := os.ReadFile(filepath.FromSlash(destination))
	if err != nil || readErr != nil || string(content) != "audio" || NewFileMover().Exists(source) {
		t.Fatalf("content = %q err = %v %v", content, err, readErr)
	}
}

func TestFileMover_Move_RefusesExistingDestination(t *testing.T) {
	folder := t.TempDir()
	source := filepath.Join(folder, "staged.m4a")
	destination := filepath.Join(folder, "existing.m4a")
	writeTestFile(t, source, "new")
	writeTestFile(t, destination, "old")

	err := NewFileMover().Move(context.Background(), source, destination)

	content, _ := os.ReadFile(destination)
	if !errors.Is(err, ErrDestinationExists) || string(content) != "old" || !NewFileMover().Exists(source) {
		t.Fatalf("err = %v content = %q", err, content)
	}
}

func TestFileMover_Move_CrossDeviceCopiesAndRemovesSource(t *testing.T) {
	folder := t.TempDir()
	source := filepath.Join(folder, "staged.m4a")
	destination := filepath.Join(folder, "out", "song.m4a")
	writeTestFile(t, source, "audio")
	mover := FileMover{rename: func(oldPath, newPath string) error {
		return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: crossDeviceErrno}
	}}

	err := mover.Move(context.Background(), source, destination)

	content, readErr := os.ReadFile(destination)
	if err != nil || readErr != nil || string(content) != "audio" || mover.Exists(source) {
		t.Fatalf("content = %q err = %v %v", content, err, readErr)
	}
}

func TestFileMover_Move_RenameFailure(t *testing.T) {
	errRename := errors.New("access denied")
	folder := t.TempDir()
	source := filepath.Join(folder, "staged.m4a")
	writeTestFile(t, source, "audio")
	mover := FileMover{rename: func(string, string) error { return errRename }}

	err := mover.Move(context.Background(), source, filepath.Join(folder, "song.m4a"))

	if !errors.Is(err, errRename) || !mover.Exists(source) {
		t.Fatalf("err = %v", err)
	}
}

func TestFileMover_Move_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := NewFileMover().Move(ctx, "a", "b")

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestFileMover_Remove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.m4a")
	writeTestFile(t, path, "x")
	mover := NewFileMover()

	err := mover.Remove(context.Background(), filepath.ToSlash(path))
	missingErr := mover.Remove(context.Background(), path)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelledErr := mover.Remove(ctx, path)

	if err != nil || mover.Exists(path) || !errors.Is(missingErr, os.ErrNotExist) || !errors.Is(cancelledErr, context.Canceled) {
		t.Fatalf("err = %v missing = %v cancelled = %v", err, missingErr, cancelledErr)
	}
}
