package infrastructure

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const runsFolderName = "runs"

type ReportStorage struct {
	output string
}

func NewReportStorage(output string) ReportStorage {
	return ReportStorage{output: output}
}

func (storage ReportStorage) Create(ctx context.Context, runID, name string) (io.WriteCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	folder := storage.runFolder(runID)
	if err := os.MkdirAll(folder, stagingFolderPermission); err != nil {
		return nil, fmt.Errorf("creating report folder: %w", err)
	}
	file, err := os.OpenFile(filepath.Clean(filepath.Join(folder, filepath.Base(name))), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, stagingFilePermission)
	if err != nil {
		return nil, fmt.Errorf("creating report %s: %w", name, err)
	}
	return file, nil
}

func (storage ReportStorage) Location(runID, name string) string {
	return filepath.Join(storage.runFolder(runID), filepath.Base(name))
}

func (storage ReportStorage) runFolder(runID string) string {
	return filepath.Join(storage.output, workFolderName, runsFolderName, runID)
}
