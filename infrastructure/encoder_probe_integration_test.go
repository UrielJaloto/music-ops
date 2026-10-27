//go:build integration

package infrastructure

import (
	"context"
	"testing"
)

func TestEncoderProbe_Integration(t *testing.T) {
	ffmpeg, _ := requireFFmpeg(t)
	requireFDKLibrary(t)
	probe := NewEncoderProbe(NewFFmpegCapabilityDetector(NewProcessRunner(), ffmpeg), NewFDKAACEncoder(nil, fdkLibraryPath()))

	report, err := probe.ProbeEncoder(context.Background())

	if err != nil || report.FFmpegVersion == "" || report.FFmpegVersion == "unknown" || !report.HasFDK {
		t.Fatalf("report = %+v err = %v", report, err)
	}
}
