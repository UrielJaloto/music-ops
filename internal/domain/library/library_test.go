package library

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/internal/domain/audio"
	"github.com/UrielJaloto/music-ops/internal/domain/credits"
	"github.com/UrielJaloto/music-ops/internal/domain/filename"
	"github.com/UrielJaloto/music-ops/internal/domain/genres"
	"github.com/UrielJaloto/music-ops/internal/domain/tracks"
)

type candidateSpec struct {
	relativePath string
	target       string
	codec        audio.Codec
	sampleRate   int
	bitDepth     int
	duration     time.Duration
}

func readyCandidate(t *testing.T, spec candidateSpec) *tracks.Track {
	t.Helper()
	quality, err := audio.NewAudioQuality(audio.AudioQualityDetails{SampleRate: spec.sampleRate, BitDepth: spec.bitDepth, Channels: 2, Duration: spec.duration})
	if err != nil {
		t.Fatal(err)
	}
	source := tracks.SourceDetails{Path: "in/" + spec.relativePath, RelativePath: spec.relativePath, Codec: spec.codec, Quality: quality}
	track, err := tracks.NewTrack(tracks.TrackID(spec.relativePath), source)
	if err != nil {
		t.Fatal(err)
	}
	mustSucceed(t, track.MarkConverted())
	mustSucceed(t, track.Resolve(credits.NewCredit(mustTitle(t, "Song"), mustArtists(t, "Band")), genres.NewUnknownClassification()))
	mustSucceed(t, track.AssignTarget(libraryTarget(t, spec.target)))
	return &track
}

func libraryTarget(t testing.TB, relative string) filename.TargetPath {
	t.Helper()
	artist, title, _ := strings.Cut(strings.TrimSuffix(relative, filename.FileExtension), " - ")
	titleValue, err := credits.NewTitle(title)
	if err != nil {
		t.Fatal(err)
	}
	artists, err := credits.NewArtistCredit([]string{artist})
	if err != nil {
		t.Fatal(err)
	}
	target, err := filename.NewTargetPath(filename.TargetPathDetails{FileName: filename.NewFileName(credits.NewCredit(titleValue, artists))})
	if err != nil {
		t.Fatal(err)
	}
	return target
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
			expected: map[string]string{"a.flac": filepath.FromSlash("lib/Band - Song.m4a")},
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
			tracks := make([]*tracks.Track, 0, len(testCase.specs))
			for _, spec := range testCase.specs {
				tracks = append(tracks, readyCandidate(t, spec))
			}

			err := NewLibrary(testCase.existing, "lib/").Admit(tracks)

			mustSucceed(t, err)
			assertKeepers(t, tracks, testCase.expected)
		})
	}
}

func assertKeepers(t *testing.T, candidates []*tracks.Track, expected map[string]string) {
	t.Helper()
	for _, track := range candidates {
		keeper := expected[track.Source().RelativePath]
		isDuplicate := track.Status() == tracks.StatusDuplicate
		if isDuplicate != (keeper != "") || track.Outcome().KeeperPath != keeper {
			t.Fatalf("%s: status %v keeper %q, want keeper %q", track.Source().RelativePath, track.Status(), track.Outcome().KeeperPath, keeper)
		}
	}
}

func TestLibraryAdmit_NotReadyTracksIgnored(t *testing.T) {
	ready := readyCandidate(t, flacSpec("a.flac"))
	discovered, err := tracks.NewTrack("id", tracks.SourceDetails{Path: "in/b.flac", RelativePath: "b.flac"})
	mustSucceed(t, err)

	err = NewLibrary([]string{"Band - Song.m4a"}, "lib").Admit([]*tracks.Track{&discovered, ready})

	mustSucceed(t, err)
	if discovered.Status() != tracks.StatusDiscovered || ready.Outcome().KeeperPath != filepath.FromSlash("lib/Band - Song.m4a") {
		t.Fatalf("discovered = %v ready keeper = %q", discovered.Status(), ready.Outcome().KeeperPath)
	}
}

func TestLibraryAdmit_EmptyBatch(t *testing.T) {
	mustSucceed(t, NewLibrary(nil, "lib").Admit(nil))
}

func BenchmarkLibraryAdmit_1000Tracks(b *testing.B) {
	library := NewLibrary([]string{"Band - Song 1.m4a"}, "lib")
	for b.Loop() {
		b.StopTimer()
		batch := benchmarkTracks(b, 1000)
		b.StartTimer()
		if err := library.Admit(batch); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkTracks(b *testing.B, count int) []*tracks.Track {
	b.Helper()
	quality, err := audio.NewAudioQuality(audio.AudioQualityDetails{SampleRate: 44100, BitDepth: 16, Channels: 2, Duration: time.Minute})
	if err != nil {
		b.Fatal(err)
	}
	title, err := credits.NewTitle("Song")
	if err != nil {
		b.Fatal(err)
	}
	artists, err := credits.NewArtistCredit([]string{"Band"})
	if err != nil {
		b.Fatal(err)
	}
	credit := credits.NewCredit(title, artists)
	batch := make([]*tracks.Track, 0, count)
	for index := range count {
		relative := strconv.Itoa(index) + ".flac"
		track, err := tracks.NewTrack(tracks.TrackID(relative), tracks.SourceDetails{Path: relative, RelativePath: relative, Codec: "flac", Quality: quality})
		if err != nil {
			b.Fatal(err)
		}
		if err := errors.Join(track.MarkConverted(), track.Resolve(credit, genres.NewUnknownClassification()), track.AssignTarget(libraryTarget(b, "Band - Song "+strconv.Itoa(index%500)+filename.FileExtension))); err != nil {
			b.Fatal(err)
		}
		batch = append(batch, &track)
	}
	return batch
}

func mustTitle(t *testing.T, raw string) credits.Title {
	t.Helper()
	title, err := credits.NewTitle(raw)
	if err != nil {
		t.Fatalf("building title %q: %v", raw, err)
	}
	return title
}

func mustArtists(t *testing.T, artists ...string) credits.ArtistCredit {
	t.Helper()
	credit, err := credits.NewArtistCredit(artists)
	if err != nil {
		t.Fatalf("building artists %v: %v", artists, err)
	}
	return credit
}
