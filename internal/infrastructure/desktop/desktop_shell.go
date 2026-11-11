package desktop

import (
	"context"
	"errors"

	"github.com/UrielJaloto/music-ops/internal/services/importing"
)

const (
	reportName           = "report.md"
	configDirPermission  = 0o750
	configFilePermission = 0o600
)

var ErrUnsupportedPlatform = errors.New("not supported on this platform")

type ReportLocator interface {
	Location(runID, name string) string
}

type DesktopShell struct {
	reports   ReportLocator
	open      func(path string) error
	playSound func() error
}

func NewDesktopShell(reports ReportLocator) DesktopShell {
	return DesktopShell{reports: reports, open: openPath, playSound: playFinishSound}
}

func (shell DesktopShell) Finish(_ context.Context, output importing.ImportOutput) error {
	return errors.Join(
		shell.playSoundIfSupported(),
		shell.open(shell.reports.Location(output.RunID, reportName)),
		shell.open(output.Output),
	)
}

func (shell DesktopShell) playSoundIfSupported() error {
	if err := shell.playSound(); !errors.Is(err, ErrUnsupportedPlatform) {
		return err
	}
	return nil
}
