package domain

import (
	"errors"
	"testing"
	"time"
)

func TestNewTrackID(t *testing.T) {
	moment := time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC)
	base := NewTrackID("a.flac", 10, moment)
	cases := []struct {
		name  string
		other TrackID
	}{
		{"PathChangesID", NewTrackID("b.flac", 10, moment)},
		{"SizeChangesID", NewTrackID("a.flac", 11, moment)},
		{"TimeChangesID", NewTrackID("a.flac", 10, moment.Add(time.Nanosecond))},
	}

	if base != NewTrackID("a.flac", 10, moment) || len(base) != 16 {
		t.Fatalf("id %q is not deterministic or has wrong length", base)
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.other == base {
				t.Fatalf("id did not change: %q", base)
			}
		})
	}
}

func TestNewTrack(t *testing.T) {
	cases := []struct {
		name          string
		source        SourceDetails
		expectedError error
	}{
		{name: "EmptyPath_ReturnsErrTrackPathMissing", source: SourceDetails{}, expectedError: ErrTrackPathMissing},
		{name: "ValidPath_IsDiscovered", source: SourceDetails{Path: "a.flac"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			track, err := NewTrack("id", testCase.source)

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v, want %v", err, testCase.expectedError)
			}
			if err == nil && (track.Status() != StatusDiscovered || track.ID() != "id" || track.Source().Path != "a.flac") {
				t.Fatalf("track = %+v", track)
			}
		})
	}
}

type trackAction struct {
	name string
	run  func(*Track) error
}

func trackActions(t *testing.T) []trackAction {
	credit := NewCredit(mustTitle(t, "Song"), mustArtists(t, "Band"))
	target := TargetPath{relative: "Band - Song.m4a"}
	return []trackAction{
		{"MarkConverted", func(track *Track) error { return track.MarkConverted() }},
		{"Resolve", func(track *Track) error { return track.Resolve(credit, UnknownClassification()) }},
		{"AssignPosition", func(track *Track) error { return track.AssignPosition(1, 1) }},
		{"AssignTarget", func(track *Track) error { return track.AssignTarget(target) }},
		{"MarkPublished", func(track *Track) error { return track.MarkPublished() }},
		{"MarkDuplicate", func(track *Track) error { return track.MarkDuplicate("keeper") }},
		{"Skip", func(track *Track) error { return track.Skip("reason") }},
		{"Fail", func(track *Track) error { return track.Fail("stage", "reason") }},
	}
}

func trackInState(t *testing.T, status TrackStatus) Track {
	t.Helper()
	track, err := NewTrack("id", SourceDetails{Path: "a.flac"})
	if err != nil {
		t.Fatal(err)
	}
	actions := trackActions(t)
	steps := map[TrackStatus][]int{
		StatusDiscovered: {},
		StatusConverted:  {0},
		StatusResolved:   {1},
		StatusReady:      {0, 1, 2, 3},
		StatusPublished:  {0, 1, 2, 3, 4},
		StatusDuplicate:  {0, 1, 5},
		StatusSkipped:    {6},
		StatusFailed:     {7},
	}
	for _, step := range steps[status] {
		if err := actions[step].run(&track); err != nil {
			t.Fatalf("preparing %v with %s: %v", status, actions[step].name, err)
		}
	}
	return track
}

func TestTrackTransitions(t *testing.T) {
	allowed := map[TrackStatus]map[string]TrackStatus{
		StatusDiscovered: {"MarkConverted": StatusConverted, "Resolve": StatusResolved, "Skip": StatusSkipped, "Fail": StatusFailed},
		StatusConverted:  {"Resolve": StatusReady, "Fail": StatusFailed},
		StatusResolved:   {"MarkConverted": StatusReady, "AssignPosition": StatusResolved, "Fail": StatusFailed},
		StatusReady:      {"AssignPosition": StatusReady, "AssignTarget": StatusReady, "MarkPublished": StatusPublished, "MarkDuplicate": StatusDuplicate, "Fail": StatusFailed},
		StatusPublished:  {},
		StatusDuplicate:  {},
		StatusSkipped:    {},
		StatusFailed:     {},
	}
	for status, allowedActions := range allowed {
		for _, action := range trackActions(t) {
			expected, isAllowed := allowedActions[action.name]
			t.Run(statusName(status)+"_"+action.name, func(t *testing.T) {
				track := trackInState(t, status)
				if action.name == "MarkPublished" && status == StatusReady {
					track.target = TargetPath{relative: "x.m4a"}
				}

				err := action.run(&track)

				assertTransition(t, track, err, transitionExpectation{isAllowed: isAllowed, expected: expected, previous: status})
			})
		}
	}
}

type transitionExpectation struct {
	isAllowed bool
	expected  TrackStatus
	previous  TrackStatus
}

func assertTransition(t *testing.T, track Track, err error, expectation transitionExpectation) {
	t.Helper()
	if !expectation.isAllowed {
		if !errors.Is(err, ErrTrackTransitionForbidden) || track.Status() != expectation.previous {
			t.Fatalf("forbidden transition: err = %v, status = %v", err, track.Status())
		}
		return
	}
	if err != nil || track.Status() != expectation.expected {
		t.Fatalf("allowed transition: err = %v, status = %v, want %v", err, track.Status(), expectation.expected)
	}
}

func statusName(status TrackStatus) string {
	return map[TrackStatus]string{
		StatusDiscovered: "Discovered", StatusConverted: "Converted", StatusResolved: "Resolved", StatusReady: "Ready",
		StatusPublished: "Published", StatusDuplicate: "Duplicate", StatusSkipped: "Skipped", StatusFailed: "Failed",
	}[status]
}

func TestMarkPublished_WithoutTarget_IsForbidden(t *testing.T) {
	track := trackInState(t, StatusReady)
	track.target = TargetPath{}

	err := track.MarkPublished()

	if !errors.Is(err, ErrTrackTransitionForbidden) || track.CanPublish() {
		t.Fatalf("err = %v", err)
	}
}

func TestAssignPosition(t *testing.T) {
	cases := []struct {
		name          string
		position      int
		total         int
		expectedError error
	}{
		{"Zero_Rejected", 0, 3, ErrTrackPositionOutOfRange},
		{"One_Accepted", 1, 3, nil},
		{"Total_Accepted", 3, 3, nil},
		{"TotalPlusOne_Rejected", 4, 3, ErrTrackPositionOutOfRange},
		{"TotalZero_Rejected", 1, 0, ErrTrackPositionOutOfRange},
		{"Negative_Rejected", -1, 3, ErrTrackPositionOutOfRange},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			track := trackInState(t, StatusResolved)

			err := track.AssignPosition(testCase.position, testCase.total)

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v, want %v", err, testCase.expectedError)
			}
			if err == nil && track.Position() != (TrackPosition{Number: testCase.position, Total: testCase.total}) {
				t.Fatalf("position = %+v", track.Position())
			}
		})
	}
}

func TestTrackOutcome(t *testing.T) {
	duplicate := trackInState(t, StatusDuplicate)
	failed := trackInState(t, StatusFailed)
	skipped := trackInState(t, StatusSkipped)

	if duplicate.Outcome().KeeperPath != "keeper" || !duplicate.IsTerminal() {
		t.Fatalf("duplicate outcome = %+v", duplicate.Outcome())
	}
	if failed.Outcome() != (TrackOutcome{FailureStage: "stage", Reason: "reason"}) {
		t.Fatalf("failed outcome = %+v", failed.Outcome())
	}
	if skipped.Outcome().Reason != "reason" {
		t.Fatalf("skipped outcome = %+v", skipped.Outcome())
	}
}

func TestTrackMetadata(t *testing.T) {
	source := SourceDetails{Path: "a.flac", Tags: SourceTags{
		Album: "Album", Date: "2001", Composer: "Composer", Copyright: "(c) Label",
		Position: SourcePosition{DiscNumber: 1, DiscTotal: 2},
	}}
	track, err := NewTrack("id", source)
	if err != nil {
		t.Fatal(err)
	}
	classification := mustClassification(t, ClassificationDetails{Genre: "Rap", IsBrazilian: true, IsKnown: true})
	credit := NewCredit(mustTitle(t, "Song"), mustArtists(t, "Main", "Guest"))
	if err := track.Resolve(credit, classification); err != nil {
		t.Fatal(err)
	}
	if err := track.AssignPosition(2, 9); err != nil {
		t.Fatal(err)
	}

	metadata := track.Metadata()

	expected := TrackMetadata{
		Title: "Song", Artist: "Main; Guest", AlbumArtist: "Main", Album: "Album", Genre: "Rap Nacional",
		Track: "2/9", Disc: "1/2", Date: "2001", Composer: "Composer", Copyright: "(c) Label",
	}
	if metadata != expected {
		t.Fatalf("metadata = %+v, want %+v", metadata, expected)
	}
	if track.Credit().Artists().String() != "Main; Guest" || track.Classification() != classification {
		t.Fatal("credit or classification not kept")
	}
}

func TestTrackMetadata_Unresolved(t *testing.T) {
	track := trackInState(t, StatusDiscovered)

	metadata := track.Metadata()

	if metadata != (TrackMetadata{Genre: "desconhecido"}) {
		t.Fatalf("metadata = %+v", metadata)
	}
}

func TestFormatPosition(t *testing.T) {
	cases := []struct {
		number, total int
		expected      string
	}{{0, 0, ""}, {1, 0, "1"}, {1, 2, "1/2"}, {-1, 2, ""}}
	for _, testCase := range cases {
		if got := formatPosition(testCase.number, testCase.total); got != testCase.expected {
			t.Errorf("formatPosition(%d, %d) = %q, want %q", testCase.number, testCase.total, got, testCase.expected)
		}
	}
}
