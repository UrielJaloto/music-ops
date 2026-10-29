package presentation

import (
	"context"
	"fmt"
	"io"
)

type VersionReport struct {
	Version       string
	FFmpegVersion string
	Engine        string
}

type VersionReporter interface {
	Report(ctx context.Context) VersionReport
}

type VersionController struct {
	reporter VersionReporter
	output   io.Writer
}

func NewVersionController(reporter VersionReporter, output io.Writer) VersionController {
	return VersionController{reporter: reporter, output: output}
}

func (controller VersionController) Execute(ctx context.Context, _ []string) int {
	report := controller.reporter.Report(ctx)
	text := fmt.Sprintf("music-ops %s\nffmpeg %s\nencoder %s\n", report.Version, report.FFmpegVersion, report.Engine)
	if _, err := io.WriteString(controller.output, text); err != nil {
		return exitFailure
	}
	return exitSuccess
}
