package domain

import (
	"strconv"
	"testing"
	"time"
)

func BenchmarkTitleWithoutFeaturing(b *testing.B) {
	title, err := NewTitle("Destination Calabria (feat. Crystal Waters & Someone Else) [Radio Edit]")
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		title.WithoutFeaturing()
	}
}

func BenchmarkLibraryAdmit_1000Tracks(b *testing.B) {
	library := NewLibrary([]string{"Band - Song 1.m4a"}, "lib")
	for b.Loop() {
		b.StopTimer()
		tracks := benchmarkTracks(b, 1000)
		b.StartTimer()
		if err := library.Admit(tracks); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkTracks(b *testing.B, count int) []*Track {
	b.Helper()
	quality, err := NewAudioQuality(AudioQualityDetails{SampleRate: 44100, BitDepth: 16, Channels: 2, Duration: time.Minute})
	if err != nil {
		b.Fatal(err)
	}
	credit := NewCredit(Title{value: "Song"}, ArtistCredit{artists: []string{"Band"}})
	tracks := make([]*Track, 0, count)
	for index := range count {
		relative := strconv.Itoa(index) + ".flac"
		track := Track{id: TrackID(relative), source: SourceDetails{Path: relative, RelativePath: relative, Codec: "flac", Quality: quality}, status: StatusReady}
		track.resolution.credit = credit
		track.target = TargetPath{relative: "Band - Song " + strconv.Itoa(index%500) + ".m4a"}
		tracks = append(tracks, &track)
	}
	return tracks
}
