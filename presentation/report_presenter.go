package presentation

import (
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/UrielJaloto/music-ops/services"
)

const (
	reportJSONName   = "report.json"
	reportMarkdown   = "report.md"
	reportJSONIndent = "  "
	failedStatus     = "failed"
)

//go:embed report.md.tmpl
var reportTemplateText string

var reportTemplate = template.Must(template.New(reportMarkdown).Parse(reportTemplateText))

var statusIcons = map[string]string{
	"published":         "✅",
	"already published": "✅",
	"skipped":           "⏭️",
	"duplicate":         "♻️",
	failedStatus:        "❌",
}

var markdownCellEscaper = strings.NewReplacer("|", `\|`, "\r\n", " ", "\n", " ")

type ReportDestination interface {
	Create(ctx context.Context, runID, name string) (io.WriteCloser, error)
	Location(runID, name string) string
}

type ReportPresenter struct {
	destination ReportDestination
}

func NewReportPresenter(destination ReportDestination) ReportPresenter {
	return ReportPresenter{destination: destination}
}

type reportCountsResponse struct {
	Published int `json:"published"`
	Duplicate int `json:"duplicate"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
	Total     int `json:"total"`
}

type reportTimingResponse struct {
	Stage    string `json:"stage"`
	Duration string `json:"duration"`
}

type reportTrackResponse struct {
	Icon   string `json:"-"`
	Source string `json:"source"`
	Target string `json:"target"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Genre  string `json:"genre"`
	Status string `json:"status"`
	Reason string `json:"error,omitempty"`
}

type reportResponse struct {
	RunID      string                 `json:"run_id"`
	Input      string                 `json:"input"`
	Output     string                 `json:"output"`
	Duration   string                 `json:"duration"`
	ExitStatus int                    `json:"exit_status"`
	Counts     reportCountsResponse   `json:"counts"`
	Timings    []reportTimingResponse `json:"timings"`
	Warnings   []string               `json:"warnings"`
	Tracks     []reportTrackResponse  `json:"tracks"`
}

func (presenter ReportPresenter) Present(ctx context.Context, output services.ImportOutput) (string, error) {
	response := toReportResponse(output)
	if err := presenter.writeFile(ctx, output.RunID, reportJSONName, func(writer io.Writer) error {
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", reportJSONIndent)
		return encoder.Encode(response)
	}); err != nil {
		return "", err
	}
	escaped := escapeReport(response)
	if err := presenter.writeFile(ctx, output.RunID, reportMarkdown, func(writer io.Writer) error {
		return reportTemplate.Execute(writer, escaped)
	}); err != nil {
		return "", err
	}
	return presenter.destination.Location(output.RunID, reportMarkdown), nil
}

func (presenter ReportPresenter) writeFile(ctx context.Context, runID, name string, write func(io.Writer) error) error {
	file, err := presenter.destination.Create(ctx, runID, name)
	if err != nil {
		return fmt.Errorf("creating %s: %w", name, err)
	}
	if err := write(file); err != nil {
		return errors.Join(fmt.Errorf("writing %s: %w", name, err), file.Close())
	}
	return file.Close()
}

func toReportResponse(output services.ImportOutput) reportResponse {
	counts := output.Counts
	return reportResponse{
		RunID: output.RunID, Input: output.Input, Output: output.Output,
		Duration: output.Duration.Round(time.Second).String(), ExitStatus: output.ExitStatus,
		Counts:   reportCountsResponse{Published: counts.Published, Duplicate: counts.Duplicate, Skipped: counts.Skipped, Failed: counts.Failed, Total: counts.Total},
		Timings:  toReportTimings(output.Timings),
		Warnings: append([]string{}, output.Warnings...),
		Tracks:   toReportTracks(output.Rows),
	}
}

func toReportTimings(timings []services.StageTiming) []reportTimingResponse {
	responses := make([]reportTimingResponse, 0, len(timings))
	for _, timing := range timings {
		responses = append(responses, reportTimingResponse{Stage: timing.Stage, Duration: timing.Duration.Round(time.Millisecond).String()})
	}
	return responses
}

func toReportTracks(rows []services.ImportRow) []reportTrackResponse {
	tracks := make([]reportTrackResponse, 0, len(rows))
	for _, row := range rows {
		tracks = append(tracks, reportTrackResponse{
			Icon: statusIcons[row.Status], Source: row.Source, Target: row.Target, Title: row.Title,
			Artist: row.Artist, Genre: row.Genre, Status: row.Status, Reason: row.Reason,
		})
	}
	slices.SortStableFunc(tracks, compareTracks)
	return tracks
}

func compareTracks(left, right reportTrackResponse) int {
	if order := cmp.Compare(failureRank(left), failureRank(right)); order != 0 {
		return order
	}
	return cmp.Compare(left.Target, right.Target)
}

func failureRank(track reportTrackResponse) int {
	if track.Status == failedStatus {
		return 0
	}
	return 1
}

func escapeReport(response reportResponse) reportResponse {
	escaped := response
	escaped.Warnings = make([]string, 0, len(response.Warnings))
	for _, warning := range response.Warnings {
		escaped.Warnings = append(escaped.Warnings, markdownCellEscaper.Replace(warning))
	}
	escaped.Tracks = make([]reportTrackResponse, 0, len(response.Tracks))
	for _, track := range response.Tracks {
		escaped.Tracks = append(escaped.Tracks, escapeTrack(track))
	}
	return escaped
}

func escapeTrack(track reportTrackResponse) reportTrackResponse {
	cells := []*string{&track.Source, &track.Target, &track.Title, &track.Artist, &track.Genre, &track.Status, &track.Reason}
	for _, cell := range cells {
		*cell = markdownCellEscaper.Replace(*cell)
	}
	return track
}
