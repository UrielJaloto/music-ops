package infrastructure

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/UrielJaloto/music-ops/services"
)

const outputProbeName = ".music-ops-write-probe"

type OutputProbe struct {
	output string
}

func NewOutputProbe(output string) OutputProbe {
	return OutputProbe{output: output}
}

func (probe OutputProbe) ProbeOutput(ctx context.Context) (services.OutputReport, error) {
	if err := ctx.Err(); err != nil {
		return services.OutputReport{}, err
	}
	report := services.OutputReport{Path: probe.output, IsWritable: isWritableFolder(probe.output)}
	free, err := freeBytes(probe.output)
	if err != nil {
		return report, fmt.Errorf("reading free space of %s: %w", probe.output, err)
	}
	report.FreeBytes = free
	return report, nil
}

func isWritableFolder(folder string) bool {
	if err := os.MkdirAll(folder, stagingFolderPermission); err != nil {
		return false
	}
	probe := filepath.Join(folder, outputProbeName)
	if err := os.WriteFile(probe, nil, stagingFilePermission); err != nil {
		return false
	}
	return os.Remove(probe) == nil
}
