//go:build !e2e

package main

import "github.com/UrielJaloto/music-ops/services"

func withTestHooks(publisher services.TrackPublisher, _ func(string) (string, bool)) services.TrackPublisher {
	return publisher
}
