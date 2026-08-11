package runtime

import (
	"context"
	"time"

	"github.com/UrielJaloto/music-ops/internal/domain/runs"
	"github.com/UrielJaloto/music-ops/internal/services/importing"
)

type ImportWorkPool interface {
	RunParallel(ctx context.Context, size int, task func(ctx context.Context, index int) error) error
}

type ImportClock interface {
	Now() time.Time
}

type ImportRunIDs interface {
	NewRunID(startedAt time.Time) (runs.RunID, error)
}

type ImportRuntime struct {
	pool     ImportWorkPool
	clock    ImportClock
	runIDs   ImportRunIDs
	progress importing.ImportProgress
}

func NewImportRuntime(pool ImportWorkPool, clock ImportClock, runIDs ImportRunIDs, progress importing.ImportProgress) ImportRuntime {
	return ImportRuntime{pool: pool, clock: clock, runIDs: runIDs, progress: progress}
}

func (runtime ImportRuntime) RunParallel(ctx context.Context, size int, task func(ctx context.Context, index int) error) error {
	return runtime.pool.RunParallel(ctx, size, task)
}

func (runtime ImportRuntime) Now() time.Time {
	return runtime.clock.Now()
}

func (runtime ImportRuntime) NewRunID(startedAt time.Time) (runs.RunID, error) {
	return runtime.runIDs.NewRunID(startedAt)
}

func (runtime ImportRuntime) Progress() importing.ImportProgress {
	return runtime.progress
}
