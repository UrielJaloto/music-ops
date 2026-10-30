package presentation

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/UrielJaloto/music-ops/services"
)

const (
	notWritten    = "not written"
	tableMinWidth = 0
	tableTabWidth = 4
	tablePadding  = 2
	tablePadChar  = ' '
)

type ErrorReporter interface {
	Present(err error) int
}

type ImportPresenter struct {
	output io.Writer
	errors ErrorReporter
}

func NewImportPresenter(output io.Writer, errors ErrorReporter) ImportPresenter {
	return ImportPresenter{output: output, errors: errors}
}

func (presenter ImportPresenter) Imported(output services.ImportOutput, reportPath string) {
	counts := output.Counts
	lines := []string{
		fmt.Sprintf("Run %s finished in %s", output.RunID, output.Duration.Round(time.Second)),
		fmt.Sprintf("  published  %d", counts.Published),
		fmt.Sprintf("  duplicate  %d", counts.Duplicate),
		fmt.Sprintf("  skipped    %d", counts.Skipped),
		fmt.Sprintf("  failed     %d", counts.Failed),
		fmt.Sprintf("  total      %d", counts.Total),
		"Report: " + reportLine(reportPath),
		fmt.Sprintf("Warnings: %d", len(output.Warnings)),
	}
	presenter.write(strings.Join(lines, "\n") + "\n")
}

func (presenter ImportPresenter) Previewed(output services.ImportPreviewOutput) {
	var table strings.Builder
	writer := tabwriter.NewWriter(&table, tableMinWidth, tableTabWidth, tablePadding, tablePadChar, 0)
	_, _ = fmt.Fprintln(writer, "SOURCE\tTARGET\tTITLE\tARTIST\tGENRE\tSTATUS")
	for _, row := range output.Rows {
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n", row.Source, row.Target, row.Title, row.Artist, row.Genre, row.Status)
	}
	_ = writer.Flush()
	_, _ = fmt.Fprintf(&table, "%d tracks planned, %d warnings\n", len(output.Rows), len(output.Warnings))
	presenter.write(table.String())
}

func reportLine(reportPath string) string {
	if reportPath == "" {
		return notWritten
	}
	return reportPath
}

func (presenter ImportPresenter) ReportFailed(err error) {
	presenter.write("warning: could not write the report: " + err.Error() + "\n")
}

func (presenter ImportPresenter) FinishFailed(err error) {
	presenter.write("warning: could not open the report: " + err.Error() + "\n")
}

func (presenter ImportPresenter) Failed(err error) int {
	return presenter.errors.Present(err)
}

func (presenter ImportPresenter) write(text string) {
	_, _ = io.WriteString(presenter.output, text)
}
