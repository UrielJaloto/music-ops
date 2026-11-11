//go:build integration

package transcoding

import (
	"context"
	"os/exec"
	"testing"

	"github.com/UrielJaloto/music-ops/internal/infrastructure/process"
)

func TestEncoderProbe_Integration(t *testing.T) {
	ffmpeg, ffprobe := requireFFmpeg(t)
	requireFDKLibrary(t)
	toolchain := NewToolchain(ToolchainSettings{FFmpeg: ffmpeg, FFprobe: ffprobe, LookPath: exec.LookPath}, process.NewProcessRunner())
	probe := NewEncoderProbe(toolchain, NewFDKAACEncoder(nil, fdkLibraryPath()))

	report, err := probe.ProbeEncoder(context.Background())

	if err != nil || report.FFmpegVersion == "" || report.FFmpegVersion == "unknown" || !report.HasFDK {
		t.Fatalf("report = %+v err = %v", report, err)
	}
}
