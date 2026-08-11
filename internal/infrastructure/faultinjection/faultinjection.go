//go:build !e2e

package faultinjection

import (
	"context"

	"github.com/UrielJaloto/music-ops/internal/services/importing"
)

func WrapPublisher(publisher importing.TrackPublisher, _ func(string) (string, bool)) importing.TrackPublisher {
	return publisher
}

func WithCancellation(ctx context.Context, _ func(string) (string, bool)) (context.Context, context.CancelFunc) {
	return context.WithCancel(ctx)
}
