//go:build e2e

package main

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"

	"github.com/UrielJaloto/music-ops/services"
)

const crashAfterVariable = "MUSIC_OPS_TEST_CRASH_AFTER"

var errTestCrash = errors.New("simulated crash after publishes")

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
	if publisher.published.Load() >= publisher.limit {
		return services.PublishOutput{}, errTestCrash
	}
	output, err := publisher.TrackPublisher.Publish(ctx, input)
	if err == nil && output.Status == services.PublishStatusPublished {
		publisher.published.Add(1)
	}
	return output, err
}
