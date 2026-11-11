package cli

import (
	"fmt"
	"io"
	"strings"
)

const (
	markdownTimingsHeader = "| Stage | Duration |\n|---|---|"
	markdownTracksHeader  = "| | Source | Target | Title | Artist | Genre | Status | Error |\n|---|---|---|---|---|---|---|---|"
	markdownNoWarnings    = "None."
)

func writeReportMarkdown(writer io.Writer, report reportResponse) error {
	sections := []string{
		renderMarkdownSummary(report),
		renderMarkdownCounts(report.Counts),
		"## Stage timings\n\n" + markdownTimingsHeader + renderMarkdownTimingRows(report.Timings),
		"## Warnings\n\n" + renderMarkdownWarnings(report.Warnings),
		"## Tracks\n\n" + markdownTracksHeader + renderMarkdownTrackRows(report.Tracks),
	}
	_, err := writer.Write([]byte(strings.Join(sections, "\n\n") + "\n"))
	return err
}

func renderMarkdownSummary(report reportResponse) string {
	return fmt.Sprintf("# music-ops report %s\n\n- Input: `%s`\n- Output: `%s`\n- Duration: %s\n- Exit status: %d",
		report.RunID, report.Input, report.Output, report.Duration, report.ExitStatus)
}

func renderMarkdownCounts(counts reportCountsResponse) string {
	return fmt.Sprintf("## Counts\n\n| Status | Tracks |\n|---|---|\n| %s published | %d |\n| %s duplicate | %d |\n| %s skipped | %d |\n| %s failed | %d |\n| total | %d |",
		statusIcons["published"], counts.Published, statusIcons["duplicate"], counts.Duplicate,
		statusIcons["skipped"], counts.Skipped, statusIcons[failedStatus], counts.Failed, counts.Total)
}

func renderMarkdownTimingRows(timings []reportTimingResponse) string {
	var rows strings.Builder
	for _, timing := range timings {
		fmt.Fprintf(&rows, "\n| %s | %s |", timing.Stage, timing.Duration)
	}
	return rows.String()
}

func renderMarkdownWarnings(warnings []string) string {
	if len(warnings) == 0 {
		return markdownNoWarnings
	}
	return "- " + strings.Join(warnings, "\n- ")
}

func renderMarkdownTrackRows(tracks []reportTrackResponse) string {
	var rows strings.Builder
	for _, track := range tracks {
		fmt.Fprintf(&rows, "\n| %s | %s | %s | %s | %s | %s | %s | %s |",
			track.Icon, track.Source, track.Target, track.Title, track.Artist, track.Genre, track.Status, track.Reason)
	}
	return rows.String()
}
