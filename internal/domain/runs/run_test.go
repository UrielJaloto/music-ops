package runs

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/internal/domain/credits"
	"github.com/UrielJaloto/music-ops/internal/domain/filename"
	"github.com/UrielJaloto/music-ops/internal/domain/genres"
	"github.com/UrielJaloto/music-ops/internal/domain/tracks"
)

var runStart = time.Date(2006, 10, 16, 15, 4, 5, 0, time.UTC)

func TestNewRunID(t *testing.T) {
	cases := []struct {
		name          string
		suffix        string
		expected      RunID
		expectedError error
	}{
		{name: "ValidSuffix_FormatsID", suffix: "a1f0", expected: "20061016-150405-a1f0"},
		{name: "ThreeChars_Rejected", suffix: "a1f", expectedError: ErrRunSuffixInvalid},
		{name: "FiveChars_Rejected", suffix: "a1f00", expectedError: ErrRunSuffixInvalid},
		{name: "Uppercase_Rejected", suffix: "A1F0", expectedError: ErrRunSuffixInvalid},
		{name: "NonHex_Rejected", suffix: "zzzz", expectedError: ErrRunSuffixInvalid},
		{name: "Empty_Rejected", suffix: "", expectedError: ErrRunSuffixInvalid},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			id, err := NewRunID(runStart, testCase.suffix)

			if !errors.Is(err, testCase.expectedError) || id != testCase.expected {
				t.Fatalf("id = %q err = %v, want %q %v", id, err, testCase.expected, testCase.expectedError)
			}
		})
	}
}

func TestNewRun(t *testing.T) {
	cases := []struct {
		name          string
		details       RunDetails
		expectedError error
	}{
		{name: "Valid_IsCreated", details: RunDetails{ID: "id", Input: "in", Output: "out", StartedAt: runStart}},
		{name: "MissingInput_ReturnsErrRunFolderMissing", details: RunDetails{Output: "out"}, expectedError: ErrRunFolderMissing},
		{name: "MissingOutput_ReturnsErrRunFolderMissing", details: RunDetails{Input: "in"}, expectedError: ErrRunFolderMissing},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			run, err := NewRun(testCase.details)

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v, want %v", err, testCase.expectedError)
			}
			if err == nil && (run.ID() != "id" || run.Input() != "in" || run.Output() != "out" || !run.StartedAt().Equal(runStart)) {
				t.Fatalf("run = %+v", run)
			}
		})
	}
}

func newTestRun(t *testing.T) Run {
	t.Helper()
	run, err := NewRun(RunDetails{ID: "id", Input: "in", Output: "out", StartedAt: runStart})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func TestRunFinish(t *testing.T) {
	cases := []struct {
		name          string
		finishes      []time.Time
		expectedError error
		expected      time.Duration
	}{
		{name: "AfterStart_SetsDuration", finishes: []time.Time{runStart.Add(time.Minute)}, expected: time.Minute},
		{name: "AtStart_IsAllowed", finishes: []time.Time{runStart}, expected: 0},
		{name: "BeforeStart_ReturnsErrRunFinishBeforeStart", finishes: []time.Time{runStart.Add(-time.Second)}, expectedError: ErrRunFinishBeforeStart},
		{name: "Twice_ReturnsErrRunAlreadyFinished", finishes: []time.Time{runStart.Add(time.Second), runStart.Add(2 * time.Second)}, expectedError: ErrRunAlreadyFinished, expected: time.Second},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			run := newTestRun(t)
			var err error

			for _, at := range testCase.finishes {
				err = run.Finish(at)
			}

			if !errors.Is(err, testCase.expectedError) || run.Duration() != testCase.expected {
				t.Fatalf("err = %v duration = %v", err, run.Duration())
			}
		})
	}
}

func TestRun_UnfinishedDurationIsZero(t *testing.T) {
	if newTestRun(t).Duration() != 0 {
		t.Fatal("unfinished run has a duration")
	}
}

func tracksInStates(t *testing.T, statuses ...tracks.TrackStatus) []*tracks.Track {
	t.Helper()
	members := make([]*tracks.Track, 0, len(statuses))
	for _, status := range statuses {
		track := trackInState(t, status)
		members = append(members, &track)
	}
	return members
}

func TestRunCounts(t *testing.T) {
	run := newTestRun(t)
	run.AddTracks(tracksInStates(t, tracks.StatusPublished, tracks.StatusPublished, tracks.StatusDuplicate))
	run.AddTracks(tracksInStates(t, tracks.StatusSkipped, tracks.StatusFailed, tracks.StatusReady))

	counts := run.Counts()

	expected := RunCounts{Published: 2, Duplicate: 1, Skipped: 1, Failed: 1, Total: 6}
	if counts != expected || len(run.Tracks()) != 6 {
		t.Fatalf("counts = %+v, want %+v", counts, expected)
	}
}

func TestRunExitStatus(t *testing.T) {
	cases := []struct {
		name     string
		statuses []tracks.TrackStatus
		expected int
	}{
		{name: "NoneFailed_ReturnsZero", statuses: []tracks.TrackStatus{tracks.StatusPublished, tracks.StatusSkipped}, expected: 0},
		{name: "OneFailed_ReturnsTwo", statuses: []tracks.TrackStatus{tracks.StatusPublished, tracks.StatusFailed}, expected: 2},
		{name: "AllDuplicates_ReturnsZero", statuses: []tracks.TrackStatus{tracks.StatusDuplicate, tracks.StatusDuplicate}, expected: 0},
		{name: "Empty_ReturnsZero", statuses: nil, expected: 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			run := newTestRun(t)
			run.AddTracks(tracksInStates(t, testCase.statuses...))

			if got := run.DecideExitStatus(); got != testCase.expected {
				t.Fatalf("ExitStatus = %d, want %d", got, testCase.expected)
			}
		})
	}
}

func TestRunWarnings(t *testing.T) {
	run := newTestRun(t)
	run.Warn("advisor unavailable")
	run.Warn("last.fm miss")

	warnings := run.Warnings()
	warnings[0] = "changed"

	if !slices.Equal(run.Warnings(), []string{"advisor unavailable", "last.fm miss"}) {
		t.Fatalf("warnings = %v", run.Warnings())
	}
	if empty := newTestRun(t).Warnings(); empty == nil || len(empty) != 0 {
		t.Fatalf("empty warnings = %#v", empty)
	}
}

func trackInState(t *testing.T, status tracks.TrackStatus) tracks.Track {
	t.Helper()
	track, err := tracks.NewTrack("id", tracks.SourceDetails{Path: "a.flac"})
	if err != nil {
		t.Fatal(err)
	}
	title, err := credits.NewTitle("Song")
	if err != nil {
		t.Fatal(err)
	}
	artists, err := credits.NewArtistCredit([]string{"Band"})
	if err != nil {
		t.Fatal(err)
	}
	credit := credits.NewCredit(title, artists)
	target, err := filename.NewTargetPath(filename.TargetPathDetails{FileName: filename.NewFileName(credit)})
	if err != nil {
		t.Fatal(err)
	}
	resolve := func() error {
		return errors.Join(track.MarkConverted(), track.Resolve(credit, genres.NewUnknownClassification()))
	}
	ready := func() error {
		return errors.Join(resolve(), track.AssignPosition(1, 1), track.AssignTarget(target))
	}
	steps := map[tracks.TrackStatus]func() error{
		tracks.StatusReady:     ready,
		tracks.StatusPublished: func() error { return errors.Join(ready(), track.MarkPublished()) },
		tracks.StatusDuplicate: func() error { return errors.Join(resolve(), track.MarkDuplicate("keeper")) },
		tracks.StatusSkipped:   func() error { return track.Skip("reason") },
		tracks.StatusFailed:    func() error { return track.Fail("stage", "reason") },
	}
	if err := steps[status](); err != nil {
		t.Fatalf("preparing %v: %v", status, err)
	}
	return track
}
