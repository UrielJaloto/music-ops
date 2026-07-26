package infrastructure

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"
)

const (
	logOperation = "operation"
	logDuration  = "duration"
	logError     = "error"
)

func NewLogger(file io.Writer, console io.Writer) *slog.Logger {
	handlers := []slog.Handler{slog.NewJSONHandler(file, nil)}
	if console != nil {
		handlers = append(handlers, slog.NewTextHandler(console, nil))
	}
	return slog.New(fanOutHandler{handlers: handlers})
}

type fanOutHandler struct {
	handlers []slog.Handler
}

func (handler fanOutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, inner := range handler.handlers {
		if inner.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (handler fanOutHandler) Handle(ctx context.Context, record slog.Record) error {
	var errs []error
	for _, inner := range handler.handlers {
		if inner.Enabled(ctx, record.Level) {
			errs = append(errs, inner.Handle(ctx, record.Clone()))
		}
	}
	return errors.Join(errs...)
}

func (handler fanOutHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	return fanOutHandler{handlers: mapHandlers(handler.handlers, func(inner slog.Handler) slog.Handler {
		return inner.WithAttrs(attributes)
	})}
}

func (handler fanOutHandler) WithGroup(name string) slog.Handler {
	return fanOutHandler{handlers: mapHandlers(handler.handlers, func(inner slog.Handler) slog.Handler {
		return inner.WithGroup(name)
	})}
}

func mapHandlers(handlers []slog.Handler, change func(slog.Handler) slog.Handler) []slog.Handler {
	changed := make([]slog.Handler, 0, len(handlers))
	for _, inner := range handlers {
		changed = append(changed, change(inner))
	}
	return changed
}

type LogClock interface {
	Now() time.Time
}

type operationLog struct {
	logger    *slog.Logger
	clock     LogClock
	operation string
	startedAt time.Time
}

func startOperationLog(ctx context.Context, logger *slog.Logger, clock LogClock, operation string) operationLog {
	logger.InfoContext(ctx, "started", logOperation, operation)
	return operationLog{logger: logger, clock: clock, operation: operation, startedAt: clock.Now()}
}

func (entry operationLog) finish(ctx context.Context, err error, attributes ...any) {
	fields := append([]any{logOperation, entry.operation, logDuration, entry.clock.Now().Sub(entry.startedAt)}, attributes...)
	if err != nil {
		entry.logger.ErrorContext(ctx, "failed", append(fields, logError, err.Error())...)
		return
	}
	entry.logger.InfoContext(ctx, "finished", fields...)
}
