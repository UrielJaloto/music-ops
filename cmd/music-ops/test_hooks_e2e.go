//go:build e2e

package main

import (
	"context"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/UrielJaloto/music-ops/services"
)

const crashAfterVariable = "MUSIC_OPS_TEST_CRASH_AFTER"

const crashExitCode = 3

func withTestHooks(publisher services.TrackPublisher, environment func(string) (string, bool)) services.TrackPublisher {
	value, _ := environment(crashAfterVariable)
	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 {
		return publisher
	}
	return crashingPublisher{TrackPublisher: publisher, limit: int64(limit), published: &atomic.Int64{}}
}

type crashingPublisher struct {
	services.TrackPublisher
	limit     int64
	published *atomic.Int64
}

func (publisher crashingPublisher) Publish(ctx context.Context, input services.PublishInput) (services.PublishOutput, error) {
	output, err := publisher.TrackPublisher.Publish(ctx, input)
	if err == nil && output.Status == services.PublishStatusPublished && publisher.published.Add(1) >= publisher.limit {
		os.Exit(crashExitCode)
	}
	return output, err
}

const cancelAfterVariable = "MUSIC_OPS_TEST_CANCEL_AFTER_MS"

func withTestCancellation(ctx context.Context, environment func(string) (string, bool)) (context.Context, context.CancelFunc) {
	value, _ := environment(cancelAfterVariable)
	milliseconds, err := strconv.Atoi(value)
	if err != nil || milliseconds <= 0 {
		return context.WithCancel(ctx)
	}
	cancellable, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(time.Duration(milliseconds)*time.Millisecond, cancel)
	return cancellable, func() {
		timer.Stop()
		cancel()
	}
}
