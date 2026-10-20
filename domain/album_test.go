package domain

import (
	"errors"
	"slices"
	"testing"
)

type albumTrackSpec struct {
	path      string
	directory string
	number    int
	total     int
	disc      int
}

func resolvedTrack(t *testing.T, spec albumTrackSpec) *Track {
	t.Helper()
	source := SourceDetails{Path: spec.path, Directory: spec.directory, Tags: SourceTags{
		Position: SourcePosition{TrackNumber: spec.number, TrackTotal: spec.total, DiscNumber: spec.disc},
	}}
	track, err := NewTrack(TrackID(spec.path), source)
	if err != nil {
		t.Fatal(err)
	}
	if err := track.Resolve(NewCredit(mustTitle(t, "Song"), mustArtists(t, "Band")), UnknownClassification()); err != nil {
		t.Fatal(err)
	}
	return &track
}

func resolvedTracks(t *testing.T, specs []albumTrackSpec) []*Track {
	t.Helper()
	tracks := make([]*Track, 0, len(specs))
	for _, spec := range specs {
		tracks = append(tracks, resolvedTrack(t, spec))
	}
	return tracks
}

func positionsByPath(tracks []*Track) map[string]TrackPosition {
	positions := map[string]TrackPosition{}
	for _, track := range tracks {
		positions[track.Source().Path] = track.Position()
	}
	return positions
}

func TestGroupByDirectory(t *testing.T) {
	tracks := resolvedTracks(t, []albumTrackSpec{
		{path: "b/1.flac", directory: "b"}, {path: "a/1.flac", directory: "a"}, {path: "b/2.flac", directory: "b"},
	})

	groups := GroupByDirectory(tracks)

	if len(groups) != 2 || groups[0].Directory() != "b" || groups[1].Directory() != "a" {
		t.Fatalf("groups = %+v", groups)
	}
	if len(groups[0].Tracks()) != 2 || groups[0].Tracks()[1].Source().Path != "b/2.flac" {
		t.Fatalf("first group tracks = %v", groups[0].Tracks())
	}
	if empty := GroupByDirectory(nil); empty == nil || len(empty) != 0 {
		t.Fatalf("empty = %#v", empty)
	}
}

func TestAlbumGroupNumber(t *testing.T) {
	cases := []struct {
		name     string
		mode     NumberingMode
		specs    []albumTrackSpec
		expected map[string]TrackPosition
	}{
		{
			name: "NUM1_RestartsPerDirectory", mode: NumberingPerDirectory,
			specs:    []albumTrackSpec{{path: "x/b.flac", directory: "x"}, {path: "x/a.flac", directory: "x"}},
			expected: map[string]TrackPosition{"x/a.flac": {1, 2}, "x/b.flac": {2, 2}},
		},
		{
			name: "NUM2_UsesUniqueSourceNumbers", mode: NumberingPerDirectory,
			specs:    []albumTrackSpec{{path: "a.flac", number: 3}, {path: "b.flac", number: 1}, {path: "c.flac", number: 2}},
			expected: map[string]TrackPosition{"b.flac": {1, 3}, "c.flac": {2, 3}, "a.flac": {3, 3}},
		},
		{
			name: "NUM2_DuplicatedSourceNumbersUseNaturalName", mode: NumberingPerDirectory,
			specs:    []albumTrackSpec{{path: "10.flac", number: 1}, {path: "2.flac", number: 1}},
			expected: map[string]TrackPosition{"2.flac": {1, 2}, "10.flac": {2, 2}},
		},
		{
			name: "NUM2_MissingSourceNumberUsesNaturalName", mode: NumberingPerDirectory,
			specs:    []albumTrackSpec{{path: "b.flac", number: 1}, {path: "a.flac", number: 0}},
			expected: map[string]TrackPosition{"a.flac": {1, 2}, "b.flac": {2, 2}},
		},
		{
			name: "NUM2_MultiDiscOrdering", mode: NumberingPerDirectory,
			specs:    []albumTrackSpec{{path: "d2t1.flac", number: 1, disc: 2}, {path: "d1t2.flac", number: 2, disc: 1}, {path: "d1t1.flac", number: 1, disc: 1}},
			expected: map[string]TrackPosition{"d1t1.flac": {1, 3}, "d1t2.flac": {2, 3}, "d2t1.flac": {3, 3}},
		},
		{
			name: "NUM4_KeepSource", mode: NumberingKeepSource,
			specs:    []albumTrackSpec{{path: "a.flac", number: 5, total: 12}},
			expected: map[string]TrackPosition{"a.flac": {5, 12}},
		},
		{
			name: "NUM4_KeepSourceWithoutTotal", mode: NumberingKeepSource,
			specs:    []albumTrackSpec{{path: "a.flac", number: 2}, {path: "b.flac", number: 1}},
			expected: map[string]TrackPosition{"a.flac": {2, 2}, "b.flac": {1, 2}},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tracks := resolvedTracks(t, testCase.specs)

			for _, group := range GroupByDirectory(tracks) {
				if err := group.Number(testCase.mode); err != nil {
					t.Fatalf("Number: %v", err)
				}
			}

			got := positionsByPath(tracks)
			for path, position := range testCase.expected {
				if got[path] != position {
					t.Fatalf("positions = %v, want %v", got, testCase.expected)
				}
			}
		})
	}
}

func TestAlbumGroupNumber_KeepSourceWithoutNumber_ReturnsError(t *testing.T) {
	tracks := resolvedTracks(t, []albumTrackSpec{{path: "a.flac"}})

	err := GroupByDirectory(tracks)[0].Number(NumberingKeepSource)

	if !errors.Is(err, ErrTrackPositionOutOfRange) {
		t.Fatalf("error = %v", err)
	}
}

func TestAlbumGroupNumber_UnresolvedTrack_ReturnsError(t *testing.T) {
	track, err := NewTrack("id", SourceDetails{Path: "a.flac"})
	if err != nil {
		t.Fatal(err)
	}

	err = GroupByDirectory([]*Track{&track})[0].Number(NumberingPerDirectory)

	if !errors.Is(err, ErrTrackTransitionForbidden) {
		t.Fatalf("error = %v", err)
	}
}

func TestCompareNatural(t *testing.T) {
	cases := []struct {
		left, right string
		expected    int
	}{
		{"2", "10", -1},
		{"10", "2", 1},
		{"a2", "a10", -1},
		{"A2", "a2", 0},
		{"B", "a", 1},
		{"track", "track1", -1},
		{"01", "1", -1},
		{"1a", "ab", -1},
		{"ab", "1a", 1},
		{"", "", 0},
	}
	for _, testCase := range cases {
		if got := compareNatural(testCase.left, testCase.right); got != testCase.expected {
			t.Errorf("compareNatural(%q, %q) = %d, want %d", testCase.left, testCase.right, got, testCase.expected)
		}
	}
}

func TestSourceFileName(t *testing.T) {
	tracks := resolvedTracks(t, []albumTrackSpec{{path: `C:\Music\Album\01 Song.flac`}, {path: "Song.flac"}})

	names := []string{sourceFileName(tracks[0]), sourceFileName(tracks[1])}

	if !slices.Equal(names, []string{"01 Song.flac", "Song.flac"}) {
		t.Fatalf("names = %v", names)
	}
}
