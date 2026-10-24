package infrastructure

import (
	"context"
	"log/slog"

	"github.com/UrielJaloto/music-ops/services"
)

type LoggedImporter interface {
	Import(ctx context.Context, input services.ImportInput) (services.ImportOutput, error)
	Preview(ctx context.Context, input services.ImportInput) (services.ImportPreviewOutput, error)
}

type ImportServiceLogger struct {
	inner  LoggedImporter
	logger *slog.Logger
	clock  LogClock
}

func NewImportServiceLogger(inner LoggedImporter, logger *slog.Logger, clock LogClock) ImportServiceLogger {
	return ImportServiceLogger{inner: inner, logger: logger, clock: clock}
}

func (decorator ImportServiceLogger) Import(ctx context.Context, input services.ImportInput) (services.ImportOutput, error) {
	entry := startOperationLog(ctx, decorator.logger, decorator.clock, "import")
	output, err := decorator.inner.Import(ctx, input)
	entry.finish(ctx, err, "run_id", output.RunID, "tracks", len(output.Rows), "exit_status", output.ExitStatus)
	return output, err
}

func (decorator ImportServiceLogger) Preview(ctx context.Context, input services.ImportInput) (services.ImportPreviewOutput, error) {
	entry := startOperationLog(ctx, decorator.logger, decorator.clock, "preview")
	output, err := decorator.inner.Preview(ctx, input)
	entry.finish(ctx, err, "tracks", len(output.Rows), "warnings", len(output.Warnings))
	return output, err
}
