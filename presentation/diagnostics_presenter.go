package presentation

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/UrielJaloto/music-ops/services"
)

const (
	checkPassed  = "✅"
	checkFailed  = "❌"
	healthyLine  = "All required checks passed."
	unhealthyFmt = "%d required check(s) failed."
)

type DiagnosticsPresenter struct {
	output io.Writer
}

func NewDiagnosticsPresenter(output io.Writer) DiagnosticsPresenter {
	return DiagnosticsPresenter{output: output}
}

func (presenter DiagnosticsPresenter) Diagnosed(output services.DiagnosticsOutput) int {
	var table strings.Builder
	writer := tabwriter.NewWriter(&table, tableMinWidth, tableTabWidth, tablePadding, tablePadChar, 0)
	failed := 0
	for _, check := range output.Checks {
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", checkIcon(check.IsOK), check.Name, check.Detail, fixText(check))
		if check.IsFatal && !check.IsOK {
			failed++
		}
	}
	_ = writer.Flush()
	table.WriteString(healthSummary(output.IsHealthy, failed) + "\n")
	if _, err := io.WriteString(presenter.output, table.String()); err != nil || !output.IsHealthy {
		return exitFailure
	}
	return exitSuccess
}

func checkIcon(isOK bool) string {
	if isOK {
		return checkPassed
	}
	return checkFailed
}

func fixText(check services.CheckResult) string {
	if check.IsOK {
		return ""
	}
	return check.Fix
}

func healthSummary(isHealthy bool, failed int) string {
	if isHealthy {
		return healthyLine
	}
	return fmt.Sprintf(unhealthyFmt, failed)
}
