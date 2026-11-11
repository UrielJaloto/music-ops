//go:build integration

package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/internal/domain/runs"
	"github.com/UrielJaloto/music-ops/internal/infrastructure/filesystem"
	"github.com/UrielJaloto/music-ops/internal/services/importing"
)

type fixedRunIDs struct {
	id  runs.RunID
	err error
}

func (ids fixedRunIDs) Now() time.Time {
	return time.Date(2026, 11, 12, 8, 30, 0, 0, time.UTC)
}

func (ids fixedRunIDs) ReserveRunID(time.Time) (runs.RunID, error) {
	return ids.id, ids.err
}

type advisorSpy struct {
	closes int
	err    error
}

func (spy *advisorSpy) Close(context.Context) error {
	spy.closes++
	return spy.err
}

type workspaceFixture struct {
	workDir string
	store   *BoltStore
	runLog  *RunLog
	advisor *advisorSpy
	runIDs  fixedRunIDs
}

func newWorkspaceFixture(workDir string) *workspaceFixture {
	return &workspaceFixture{
		workDir: workDir,
		store:   NewBoltStore(filepath.Join(workDir, "state.db")),
		runLog:  NewRunLog(NewReportStorage(workDir)),
		advisor: &advisorSpy{},
		runIDs:  fixedRunIDs{id: "20261112-083000-abcd"},
	}
}

func (fixture *workspaceFixture) workspace() Workspace {
	return NewWorkspace(fixture.workDir, WorkspaceParts{
		Store: fixture.store, Staging: filesystem.NewStagingArea(fixture.workDir), RunLog: fixture.runLog,
		Advisor: fixture.advisor, RunIDs: fixture.runIDs,
	})
}

func TestWorkspace_OpenUseClose(t *testing.T) {
	fixture := newWorkspaceFixture(t.TempDir())
	workspace := fixture.workspace()
	staging := filepath.Join(fixture.workDir, "staging", "20261112-083000-abcd")
	journal := NewJournalRepository(fixture.store)
	ctx := context.Background()

	openErr := workspace.Open(ctx)
	_, writeErr := fixture.runLog.Write([]byte("entry\n"))
	recordErr := journal.RecordConverted(ctx, "t1", "staged.m4a")
	_, stagingErr := os.Stat(staging)
	closeErr := workspace.Close(ctx)
	_, closedErr := journal.IsConverted(ctx, "t1")

	if err := errors.Join(openErr, writeErr, recordErr, stagingErr, closeErr); err != nil {
		t.Fatal(err)
	}
	logged, err := os.ReadFile(filepath.Join(fixture.workDir, "runs", "20261112-083000-abcd", "log.jsonl"))
	if err != nil || string(logged) != "entry\n" {
		t.Fatalf("log = %q err = %v", logged, err)
	}
	if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) || fixture.advisor.closes != 1 {
		t.Fatalf("staging err = %v advisor closes = %d", err, fixture.advisor.closes)
	}
	if !errors.Is(closedErr, importing.ErrWorkspaceClosed) {
		t.Fatalf("closed err = %v", closedErr)
	}
}

func TestWorkspace_ReopenKeepsState(t *testing.T) {
	fixture := newWorkspaceFixture(t.TempDir())
	journal := NewJournalRepository(fixture.store)
	ctx := context.Background()
	first := fixture.workspace()
	if err := errors.Join(first.Open(ctx), journal.RecordConverted(ctx, "t1", "a.m4a"), first.Close(ctx)); err != nil {
		t.Fatal(err)
	}
	second := fixture.workspace()

	openErr := second.Open(ctx)
	isConverted, readErr := journal.IsConverted(ctx, "t1")
	closeErr := second.Close(ctx)

	if err := errors.Join(openErr, readErr, closeErr); err != nil || !isConverted {
		t.Fatalf("converted = %v err = %v", isConverted, err)
	}
}

func TestWorkspace_Open_Failures(t *testing.T) {
	errRunID := errors.New("no randomness")
	cases := []struct {
		name          string
		arrange       func(t *testing.T, fixture *workspaceFixture)
		expectedError error
	}{
		{name: "WorkFolderUnknown", arrange: func(_ *testing.T, fixture *workspaceFixture) { fixture.workDir = "" }, expectedError: ErrWorkFolderUnknown},
		{name: "RunIDFails", arrange: func(_ *testing.T, fixture *workspaceFixture) { fixture.runIDs.err = errRunID }, expectedError: errRunID},
		{name: "StagingFolderIsAFile", arrange: func(t *testing.T, fixture *workspaceFixture) {
			writeWorkspaceFile(t, filepath.Join(fixture.workDir, "staging"))
		}},
		{name: "StateFileIsAFolder", arrange: func(t *testing.T, fixture *workspaceFixture) {
			if err := os.MkdirAll(filepath.Join(fixture.workDir, "state.db"), 0o750); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "ReportsFolderIsAFile", arrange: func(t *testing.T, fixture *workspaceFixture) {
			writeWorkspaceFile(t, filepath.Join(fixture.workDir, "runs"))
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newWorkspaceFixture(t.TempDir())
			testCase.arrange(t, fixture)

			err := fixture.workspace().Open(context.Background())

			if err == nil || testCase.expectedError != nil && !errors.Is(err, testCase.expectedError) {
				t.Fatalf("err = %v", err)
			}
			if _, writeErr := fixture.runLog.Write([]byte("x")); !errors.Is(writeErr, importing.ErrWorkspaceClosed) {
				t.Fatalf("log still open: %v", writeErr)
			}
			assertNoStagedRun(t, fixture.workDir)
		})
	}
}

func TestWorkspace_SecondOpenWhileFirstIsOpen_ReturnsErrRunInProgress(t *testing.T) {
	workDir := t.TempDir()
	first := newWorkspaceFixture(workDir).workspace()
	if err := first.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close(context.Background()) })
	second := newWorkspaceFixture(workDir)
	second.runIDs.id = "20261112-083001-beef"

	err := second.workspace().Open(context.Background())

	if !errors.Is(err, importing.ErrRunInProgress) {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(workDir, "staging", "20261112-083001-beef")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("second staging left behind: %v", statErr)
	}
}

func TestWorkspace_Close_WithoutOpen_OnlyStopsTheAdvisor(t *testing.T) {
	fixture := newWorkspaceFixture(t.TempDir())
	errStop := errors.New("stop failed")
	fixture.advisor.err = errStop

	err := fixture.workspace().Close(context.Background())

	if !errors.Is(err, errStop) || fixture.advisor.closes != 1 {
		t.Fatalf("err = %v closes = %d", err, fixture.advisor.closes)
	}
}

func TestRunLog_ClosedLifecycle(t *testing.T) {
	runLog := NewRunLog(NewReportStorage(t.TempDir()))

	_, beforeErr := runLog.Write([]byte("x"))
	firstClose := runLog.Close()
	openErr := runLog.Open(context.Background(), "r1")
	closeErr := runLog.Close()
	_, afterErr := runLog.Write([]byte("x"))

	if !errors.Is(beforeErr, importing.ErrWorkspaceClosed) || !errors.Is(afterErr, importing.ErrWorkspaceClosed) {
		t.Fatalf("errors = %v %v", beforeErr, afterErr)
	}
	if err := errors.Join(firstClose, openErr, closeErr); err != nil {
		t.Fatal(err)
	}
}

func TestBoltStore_Closed_ReturnsErrWorkspaceClosed(t *testing.T) {
	store := NewBoltStore(filepath.Join(t.TempDir(), "state.db"))
	cache := NewBoltCache(store, &movableClock{}, "lastfm")
	ctx := context.Background()

	_, _, readErr := cache.Get(ctx, "key")
	writeErr := cache.Put(ctx, "key", []byte("v"), time.Hour)
	deleteErr := store.delete(ctx, "bucket", "key")
	closeErr := store.Close()

	for _, err := range []error{readErr, writeErr, deleteErr} {
		if !errors.Is(err, importing.ErrWorkspaceClosed) {
			t.Fatalf("err = %v", err)
		}
	}
	if closeErr != nil {
		t.Fatalf("close err = %v", closeErr)
	}
}

func writeWorkspaceFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertNoStagedRun(t *testing.T, workDir string) {
	t.Helper()
	staged, _ := filepath.Glob(filepath.Join(workDir, "staging", "*"))
	if len(staged) != 0 {
		t.Fatalf("staging left behind: %v", staged)
	}
}
