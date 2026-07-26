package infrastructure

import (
	"context"
	"log/slog"

	"github.com/UrielJaloto/music-ops/services"
)

type LoggedPublisher interface {
	FindPublished(ctx context.Context, input services.FindPublishedInput) (services.PublishedTracks, error)
	Admit(ctx context.Context, input services.AdmitInput) (services.AdmittedTracks, error)
	Publish(ctx context.Context, input services.PublishInput) (services.PublishOutput, error)
}

type PublishingServiceLogger struct {
	inner  LoggedPublisher
	logger *slog.Logger
	clock  LogClock
}

func NewPublishingServiceLogger(inner LoggedPublisher, logger *slog.Logger, clock LogClock) PublishingServiceLogger {
	return PublishingServiceLogger{inner: inner, logger: logger, clock: clock}
}

func (decorator PublishingServiceLogger) FindPublished(ctx context.Context, input services.FindPublishedInput) (services.PublishedTracks, error) {
	entry := startOperationLog(ctx, decorator.logger, decorator.clock, "find_published")
	output, err := decorator.inner.FindPublished(ctx, input)
	entry.finish(ctx, err, "tracks", len(input.IDs), "published", len(output.Targets))
	return output, err
}

func (decorator PublishingServiceLogger) Admit(ctx context.Context, input services.AdmitInput) (services.AdmittedTracks, error) {
	entry := startOperationLog(ctx, decorator.logger, decorator.clock, "admit")
	output, err := decorator.inner.Admit(ctx, input)
	entry.finish(ctx, err, "tracks", len(input.Tracks), "publishable", len(output.Publishable), "duplicates", len(output.Duplicates))
	return output, err
}

func (decorator PublishingServiceLogger) Publish(ctx context.Context, input services.PublishInput) (services.PublishOutput, error) {
	entry := startOperationLog(ctx, decorator.logger, decorator.clock, "publish")
	output, err := decorator.inner.Publish(ctx, input)
	entry.finish(ctx, err, "track", string(input.Track.ID), "status", output.Status, "destination", output.Destination)
	return output, err
}
