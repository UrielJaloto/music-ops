package storage

import (
	"context"
	"io"
	"sync"

	"github.com/UrielJaloto/music-ops/internal/services/importing"
)

const runLogName = "log.jsonl"

type RunLog struct {
	reports ReportStorage
	mutex   sync.Mutex
	file    io.WriteCloser
}

func NewRunLog(reports ReportStorage) *RunLog {
	return &RunLog{reports: reports}
}

func (log *RunLog) Open(ctx context.Context, runID string) error {
	file, err := log.reports.Create(ctx, runID, runLogName)
	if err != nil {
		return err
	}
	log.mutex.Lock()
	defer log.mutex.Unlock()
	log.file = file
	return nil
}

func (log *RunLog) Write(entry []byte) (int, error) {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	if log.file == nil {
		return 0, importing.ErrWorkspaceClosed
	}
	return log.file.Write(entry)
}

func (log *RunLog) Close() error {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	if log.file == nil {
		return nil
	}
	err := log.file.Close()
	log.file = nil
	return err
}
