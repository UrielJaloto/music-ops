package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/UrielJaloto/music-ops/internal/domain/runs"
)

var ErrWorkFolderUnknown = errors.New("the user work folder is unknown")

type WorkspaceStaging interface {
	Prepare(id runs.RunID) error
	RemoveOtherRuns() error
	Cleanup(ctx context.Context) error
}

type WorkspaceProcess interface {
	Close(ctx context.Context) error
}

type RunIDSource interface {
	Now() time.Time
	ReserveRunID(startedAt time.Time) (runs.RunID, error)
}

type WorkspaceParts struct {
	Store   *BoltStore
	Staging WorkspaceStaging
	RunLog  *RunLog
	Advisor WorkspaceProcess
	RunIDs  RunIDSource
}

type Workspace struct {
	workDir string
	parts   WorkspaceParts
}

func NewWorkspace(workDir string, parts WorkspaceParts) Workspace {
	return Workspace{workDir: workDir, parts: parts}
}

func (workspace Workspace) Open(ctx context.Context) error {
	if workspace.workDir == "" {
		return ErrWorkFolderUnknown
	}
	runID, err := workspace.parts.RunIDs.ReserveRunID(workspace.parts.RunIDs.Now())
	if err != nil {
		return fmt.Errorf("creating run id: %w", err)
	}
	if err := workspace.parts.Staging.Prepare(runID); err != nil {
		return err
	}
	if err := workspace.parts.Store.Open(); err != nil {
		return errors.Join(err, workspace.parts.Staging.Cleanup(ctx))
	}
	if err := workspace.openRun(ctx, runID); err != nil {
		return errors.Join(err, workspace.Close(ctx))
	}
	return nil
}

func (workspace Workspace) openRun(ctx context.Context, runID runs.RunID) error {
	if err := workspace.parts.Staging.RemoveOtherRuns(); err != nil {
		return err
	}
	return workspace.parts.RunLog.Open(ctx, string(runID))
}

func (workspace Workspace) Close(ctx context.Context) error {
	return errors.Join(
		workspace.parts.RunLog.Close(),
		workspace.parts.Staging.Cleanup(ctx),
		workspace.parts.Store.Close(),
		workspace.parts.Advisor.Close(ctx),
	)
}
