package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/UrielJaloto/music-ops/internal/services/settings"
)

var errWrite = errors.New("write failed")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errWrite
}

func TestOutputWriteFails_ReturnsFailure(t *testing.T) {
	cases := []struct {
		name    string
		execute func() int
	}{
		{name: "ErrorPresenter", execute: func() int { return NewErrorPresenter(failingWriter{}).Present(errWrite) }},
		{name: "CommandRouterUsage", execute: func() int {
			return NewCommandRouter(map[string]CommandController{}, failingWriter{}).Route(context.Background(), RouterRequest{Command: "unknown"})
		}},
		{name: "SettingsPresenter", execute: func() int {
			return NewSettingsPresenter(failingWriter{}, NewErrorPresenter(failingWriter{})).Located(settings.SettingsLocationOutput{Path: "C:/config.yaml"})
		}},
		{name: "VersionController", execute: func() int {
			return NewVersionController(versionReporterStub{}, failingWriter{}).Execute(context.Background(), nil)
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if code := testCase.execute(); code != exitFailure {
				t.Fatalf("code = %d, want %d", code, exitFailure)
			}
		})
	}
}
