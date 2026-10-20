package domain

import (
	"regexp"
	"slices"
	"time"
)

var (
	ErrRunSuffixInvalid     = newBusinessError("run id suffix must be 4 lowercase hex characters")
	ErrRunFolderMissing     = newBusinessError("run input or output folder is missing")
	ErrRunFinishBeforeStart = newBusinessError("run cannot finish before it starts")
	ErrRunAlreadyFinished   = newBusinessError("run already finished")
)

const (
	runIDLayout          = "20060102-150405"
	runIDSeparator       = "-"
	ExitStatusSuccess    = 0
	ExitStatusWithFailed = 2
)

var runSuffixPattern = regexp.MustCompile(`^[0-9a-f]{4}$`)

type RunID string

func NewRunID(startedAt time.Time, suffix string) (RunID, error) {
	if !runSuffixPattern.MatchString(suffix) {
		return "", ErrRunSuffixInvalid
	}
	return RunID(startedAt.Format(runIDLayout) + runIDSeparator + suffix), nil
}

type RunDetails struct {
	ID        RunID
	Input     string
	Output    string
	StartedAt time.Time
}

type RunCounts struct {
	Published int
	Duplicate int
	Skipped   int
	Failed    int
	Total     int
}

type Run struct {
	id         RunID
	input      string
	output     string
	startedAt  time.Time
	finishedAt time.Time
	tracks     []*Track
	warnings   []string
}

func NewRun(details RunDetails) (Run, error) {
	if details.Input == "" || details.Output == "" {
		return Run{}, ErrRunFolderMissing
	}
	return Run{id: details.ID, input: details.Input, output: details.Output, startedAt: details.StartedAt, tracks: []*Track{}, warnings: []string{}}, nil
}

func (run *Run) AddTracks(tracks []*Track) {
	run.tracks = append(run.tracks, tracks...)
}

func (run *Run) Warn(message string) {
	run.warnings = append(run.warnings, message)
}

func (run *Run) Finish(at time.Time) error {
	if !run.finishedAt.IsZero() {
		return ErrRunAlreadyFinished
	}
	if at.Before(run.startedAt) {
		return ErrRunFinishBeforeStart
	}
	run.finishedAt = at
	return nil
}

func (run Run) ID() RunID {
	return run.id
}

func (run Run) Input() string {
	return run.input
}

func (run Run) Output() string {
	return run.output
}

func (run Run) StartedAt() time.Time {
	return run.startedAt
}

func (run Run) Counts() RunCounts {
	counts := RunCounts{Total: len(run.tracks)}
	for _, track := range run.tracks {
		counts = countTrack(counts, track.Status())
	}
	return counts
}

func (run Run) ExitStatus() int {
	if run.Counts().Failed > 0 {
		return ExitStatusWithFailed
	}
	return ExitStatusSuccess
}

func (run Run) Duration() time.Duration {
	if run.finishedAt.IsZero() {
		return 0
	}
	return run.finishedAt.Sub(run.startedAt)
}

func (run Run) Tracks() []*Track {
	return slices.Clone(run.tracks)
}

func (run Run) Warnings() []string {
	return slices.Clone(run.warnings)
}

func countTrack(counts RunCounts, status TrackStatus) RunCounts {
	switch status {
	case StatusPublished:
		counts.Published++
	case StatusDuplicate:
		counts.Duplicate++
	case StatusSkipped:
		counts.Skipped++
	case StatusFailed:
		counts.Failed++
	}
	return counts
}
