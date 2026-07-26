package infrastructure

import (
	"context"
	"log/slog"

	"github.com/UrielJaloto/music-ops/services"
)

type LoggedConverter interface {
	Convert(ctx context.Context, input services.ConvertInput) (services.ConvertOutput, error)
}

type ConversionServiceLogger struct {
	inner  LoggedConverter
	logger *slog.Logger
	clock  LogClock
}

func NewConversionServiceLogger(inner LoggedConverter, logger *slog.Logger, clock LogClock) ConversionServiceLogger {
	return ConversionServiceLogger{inner: inner, logger: logger, clock: clock}
}

func (decorator ConversionServiceLogger) Convert(ctx context.Context, input services.ConvertInput) (services.ConvertOutput, error) {
	entry := startOperationLog(ctx, decorator.logger, decorator.clock, "convert")
	output, err := decorator.inner.Convert(ctx, input)
	entry.finish(ctx, err, "tracks", len(input.Tracks), "converted", len(output.Converted), "failed", len(output.Failed))
	return output, err
}
