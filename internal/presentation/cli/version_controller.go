package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/UrielJaloto/music-ops/internal/services/diagnostics"
)

const ffmpegNotFound = "ffmpeg not found"

type VersionReporter interface {
	ReportVersion(ctx context.Context) diagnostics.VersionOutput
}

type VersionController struct {
	reporter VersionReporter
	output   io.Writer
}

func NewVersionController(reporter VersionReporter, output io.Writer) VersionController {
	return VersionController{reporter: reporter, output: output}
}

func (controller VersionController) Execute(ctx context.Context, _ []string) int {
	if _, err := io.WriteString(controller.output, formatVersion(controller.reporter.ReportVersion(ctx))); err != nil {
		return exitFailure
	}
	return exitSuccess
}

func formatVersion(version diagnostics.VersionOutput) string {
	if !version.IsFFmpegFound {
		return fmt.Sprintf("music-ops %s\n%s\n", version.Version, ffmpegNotFound)
	}
	return fmt.Sprintf("music-ops %s\nffmpeg %s\nencoder %s %d kb/s\n", version.Version, version.FFmpegVersion, version.Engine, version.BitrateKbps)
}
