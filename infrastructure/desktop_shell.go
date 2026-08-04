package infrastructure

import (
	"context"
	"errors"

	"github.com/UrielJaloto/music-ops/services"
)

const (
	powershellExecutable = "powershell"
	openerExecutable     = "rundll32"
	finishSound          = "[System.Media.SystemSounds]::Asterisk.Play()"
	reportName           = "report.md"
)

type ReportLocator interface {
	Location(runID, name string) string
}

type DesktopShell struct {
	runner  ProcessExecutor
	reports ReportLocator
}

func NewDesktopShell(runner ProcessExecutor, reports ReportLocator) DesktopShell {
	return DesktopShell{runner: runner, reports: reports}
}

func (shell DesktopShell) Finish(ctx context.Context, output services.ImportOutput) error {
	var errs []error
	for _, request := range finishCommands(shell.reports.Location(output.RunID, reportName), output.Output) {
		if _, err := shell.runner.Run(ctx, request); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func finishCommands(reportPath, outputFolder string) []ProcessRequest {
	return []ProcessRequest{
		{Executable: powershellExecutable, Arguments: []string{"-NoProfile", "-Command", finishSound}},
		{Executable: openerExecutable, Arguments: []string{"url.dll,FileProtocolHandler", reportPath}},
		{Executable: openerExecutable, Arguments: []string{"url.dll,FileProtocolHandler", outputFolder}},
	}
}
