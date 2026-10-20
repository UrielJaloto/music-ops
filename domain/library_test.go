package domain

import (
	"testing"
	"time"
)

type candidateSpec struct {
	relativePath string
	target       string
	codec        Codec
	sampleRate   int
	bitDepth     int
	duration     time.Duration
}

func readyCandidate(t *testing.T, spec candidateSpec) *Track {
	t.Helper()
	quality, err := NewAudioQuality(AudioQualityDetails{SampleRate: spec.sampleRate, BitDepth: spec.bitDepth, Channels: 2, Duration: spec.duration})
	if err != nil {
		t.Fatal(err)
	}
	source := SourceDetails{Path: "in/" + spec.relativePath, RelativePath: spec.relativePath, Codec: spec.codec, Quality: quality}
	track, err := NewTrack(TrackID(spec.relativePath), source)
	if err != nil {
		t.Fatal(err)
	}
	mustSucceed(t, track.MarkConverted())
	mustSucceed(t, track.Resolve(NewCredit(mustTitle(t, "Song"), mustArtists(t, "Band")), UnknownClassification()))
	mustSucceed(t, track.AssignTarget(TargetPath{relative: spec.target}))
	return &track
}

func mustSucceed(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func flacSpec(relativePath string) candidateSpec {
	return candidateSpec{relativePath: relativePath, target: "Band - Song.m4a", codec: "flac", sampleRate: 44100, bitDepth: 16, duration: time.Minute}
}

func specWith(relativePath string, change func(*candidateSpec)) candidateSpec {
	spec := flacSpec(relativePath)
	change(&spec)
	return spec
}

func TestLibraryAdmit(t *testing.T) {
	cases := []struct {
		name     string
		existing []string
		specs    []candidateSpec
		expected map[string]string
	}{
		{
			name: "DUP2_ExistingFileWins", existing: []string{"Band - Song.m4a"},
			specs:    []candidateSpec{flacSpec("a.flac")},
			expected: map[string]string{"a.flac": "lib/Band - Song.m4a"},
		},
		{
			name:     "DUP3_LosslessBeatsLossy",
			specs:    []candidateSpec{specWith("a.mp3", func(s *candidateSpec) { s.codec = "mp3"; s.sampleRate = 48000 }), flacSpec("b.flac")},
			expected: map[string]string{"a.mp3": "in/b.flac", "b.flac": ""},
		},
		{
			name:     "DUP3_HigherSampleRateWins",
			specs:    []candidateSpec{flacSpec("a.flac"), specWith("b.flac", func(s *candidateSpec) { s.sampleRate = 96000 })},
			expected: map[string]string{"a.flac": "in/b.flac", "b.flac": ""},
		},
		{
			name:     "DUP3_HigherBitDepthWins",
			specs:    []candidateSpec{flacSpec("a.flac"), specWith("b.flac", func(s *candidateSpec) { s.bitDepth = 24 })},
			expected: map[string]string{"a.flac": "in/b.flac", "b.flac": ""},
		},
		{
			name:     "DUP3_LongerDurationWins",
			specs:    []candidateSpec{specWith("a.flac", func(s *candidateSpec) { s.duration = 2 * time.Minute }), flacSpec("b.flac")},
			expected: map[string]string{"a.flac": "", "b.flac": "in/a.flac"},
		},
		{
			name:     "DUP3_PathOrderBreaksTie",
			specs:    []candidateSpec{flacSpec("b.flac"), flacSpec("a.flac")},
			expected: map[string]string{"a.flac": "", "b.flac": "in/a.flac"},
		},
		{
			name:     "DUP3_ThreeWayDuplicate",
			specs:    []candidateSpec{flacSpec("a.flac"), specWith("b.flac", func(s *candidateSpec) { s.sampleRate = 96000 }), specWith("c.mp3", func(s *candidateSpec) { s.codec = "mp3" })},
			expected: map[string]string{"a.flac": "in/b.flac", "b.flac": "", "c.mp3": "in/b.flac"},
		},
		{
			name:     "DUP1_CaseAndSpacingOnlyDifference",
			specs:    []candidateSpec{flacSpec("a.flac"), specWith("b.flac", func(s *candidateSpec) { s.target = "band  -  SONG.m4a" })},
			expected: map[string]string{"a.flac": "", "b.flac": "in/a.flac"},
		},
		{
			name:     "DifferentTargets_BothKept",
			specs:    []candidateSpec{flacSpec("a.flac"), specWith("b.flac", func(s *candidateSpec) { s.target = "Band - Other.m4a" })},
			expected: map[string]string{"a.flac": "", "b.flac": ""},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tracks := make([]*Track, 0, len(testCase.specs))
			for _, spec := range testCase.specs {
				tracks = append(tracks, readyCandidate(t, spec))
			}

			err := NewLibrary(testCase.existing, "lib/").Admit(tracks)

			mustSucceed(t, err)
			assertKeepers(t, tracks, testCase.expected)
		})
	}
}

func assertKeepers(t *testing.T, tracks []*Track, expected map[string]string) {
	t.Helper()
	for _, track := range tracks {
		keeper := expected[track.Source().RelativePath]
		isDuplicate := track.Status() == StatusDuplicate
		if isDuplicate != (keeper != "") || track.Outcome().KeeperPath != keeper {
			t.Fatalf("%s: status %v keeper %q, want keeper %q", track.Source().RelativePath, track.Status(), track.Outcome().KeeperPath, keeper)
		}
	}
}

func TestLibraryAdmit_NotReadyTracksIgnored(t *testing.T) {
	ready := readyCandidate(t, flacSpec("a.flac"))
	discovered, err := NewTrack("id", SourceDetails{Path: "in/b.flac", RelativePath: "b.flac"})
	mustSucceed(t, err)

	err = NewLibrary([]string{"Band - Song.m4a"}, "lib").Admit([]*Track{&discovered, ready})

	mustSucceed(t, err)
	if discovered.Status() != StatusDiscovered || ready.Outcome().KeeperPath != "lib/Band - Song.m4a" {
		t.Fatalf("discovered = %v ready keeper = %q", discovered.Status(), ready.Outcome().KeeperPath)
	}
}

func TestLibraryAdmit_EmptyBatch(t *testing.T) {
	mustSucceed(t, NewLibrary(nil, "lib").Admit(nil))
}
