//go:build !e2e

package main

import (
	"context"

	"github.com/UrielJaloto/music-ops/services"
)

func withTestHooks(publisher services.TrackPublisher, _ func(string) (string, bool)) services.TrackPublisher {
	return publisher
}

func withTestCancellation(ctx context.Context, _ func(string) (string, bool)) (context.Context, context.CancelFunc) {
	return context.WithCancel(ctx)
}
