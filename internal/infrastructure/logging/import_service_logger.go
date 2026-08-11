package logging

import (
	"context"
	"log/slog"

	"github.com/UrielJaloto/music-ops/internal/services/importing"
)

type LoggedImporter interface {
	Import(ctx context.Context, input importing.ImportInput) (importing.ImportOutput, error)
	Preview(ctx context.Context, input importing.ImportInput) (importing.ImportPreviewOutput, error)
}

type ImportServiceLogger struct {
	inner  LoggedImporter
	logger *slog.Logger
	clock  LogClock
}

func NewImportServiceLogger(inner LoggedImporter, logger *slog.Logger, clock LogClock) ImportServiceLogger {
	return ImportServiceLogger{inner: inner, logger: logger, clock: clock}
}

func (decorator ImportServiceLogger) Import(ctx context.Context, input importing.ImportInput) (importing.ImportOutput, error) {
	entry := startOperationLog(ctx, decorator.logger, decorator.clock, "import")
	output, err := decorator.inner.Import(ctx, input)
	entry.finish(ctx, err, "run_id", output.RunID, "tracks", len(output.Rows), "exit_status", output.ExitStatus)
	return output, err
}

func (decorator ImportServiceLogger) Preview(ctx context.Context, input importing.ImportInput) (importing.ImportPreviewOutput, error) {
	entry := startOperationLog(ctx, decorator.logger, decorator.clock, "preview")
	output, err := decorator.inner.Preview(ctx, input)
	entry.finish(ctx, err, "tracks", len(output.Rows), "warnings", len(output.Warnings))
	return output, err
}
