package desktop

import (
	"context"
	"errors"

	"github.com/UrielJaloto/music-ops/internal/infrastructure/process"
	"github.com/UrielJaloto/music-ops/internal/services/importing"
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
	runner  ShellRunner
	reports ReportLocator
}

func NewDesktopShell(runner ShellRunner, reports ReportLocator) DesktopShell {
	return DesktopShell{runner: runner, reports: reports}
}

func (shell DesktopShell) Finish(ctx context.Context, output importing.ImportOutput) error {
	var errs []error
	for _, request := range finishCommands(shell.reports.Location(output.RunID, reportName), output.Output) {
		if _, err := shell.runner.Run(ctx, request); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func finishCommands(reportPath, outputFolder string) []process.ProcessRequest {
	return []process.ProcessRequest{
		{Executable: powershellExecutable, Arguments: []string{"-NoProfile", "-Command", finishSound}},
		{Executable: openerExecutable, Arguments: []string{"url.dll,FileProtocolHandler", reportPath}},
		{Executable: openerExecutable, Arguments: []string{"url.dll,FileProtocolHandler", outputFolder}},
	}
}

const configDirPermission = 0o750

const configFilePermission = 0o600

type ShellRunner interface {
	Run(ctx context.Context, request process.ProcessRequest) (process.ProcessResult, error)
}
