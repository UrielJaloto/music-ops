package infrastructure

import (
	"context"
	"runtime"

	"golang.org/x/sync/errgroup"
)

const minimumPoolLimit = 1

type WorkerPool struct {
	limit int
}

func NewWorkerPool(limit int) WorkerPool {
	return WorkerPool{limit: limit}
}

func (pool WorkerPool) RunParallel(ctx context.Context, size int, task func(ctx context.Context, index int) error) error {
	group, groupContext := errgroup.WithContext(ctx)
	group.SetLimit(pool.effectiveLimit())
	for index := range size {
		if groupContext.Err() != nil {
			break
		}
		group.Go(func() error {
			return task(groupContext, index)
		})
	}
	if err := group.Wait(); err != nil {
		return err
	}
	return ctx.Err()
}

func (pool WorkerPool) effectiveLimit() int {
	if pool.limit > 0 {
		return pool.limit
	}
	return max(runtime.NumCPU()-1, minimumPoolLimit)
}
