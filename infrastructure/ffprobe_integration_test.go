//go:build integration

package infrastructure

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
)

func TestFFprobeInspector_Integration(t *testing.T) {
	_, ffprobe := requireFFmpeg(t)
	tags := audioFixtureTags{title: "Bênção (feat. Guest)", artist: "Main", albumArtist: "Main", album: "Album", date: "2024", track: 5, trackTotal: 50, disc: 1}
	fixtures := []audioFixture{
		{relativePath: "a.flac", format: "flac", tags: tags, signal: audioFixtureSignal{sampleRate: 96000, channels: 2, bitDepth: 24, seconds: 2}, hasCover: true},
		{relativePath: "b.m4a", format: "m4a", tags: tags, signal: audioFixtureSignal{sampleRate: 44100, channels: 2, seconds: 2}},
		{relativePath: "c.opus", format: "opus", tags: tags, signal: audioFixtureSignal{sampleRate: 48000, channels: 1, seconds: 2}},
	}
	paths := buildAudioFixtures(t, t.TempDir(), fixtures)
	inspector := NewFFprobeInspector(NewProcessRunner(), ffprobe)
	expectedTags := domain.SourceTags{Title: tags.title, Artist: "Main", AlbumArtist: "Main", Album: "Album", Date: "2024", Position: domain.SourcePosition{TrackNumber: 5, TrackTotal: 50, DiscNumber: 1}}

	for index, path := range paths {
		inspected, err := inspector.Inspect(context.Background(), path)

		if err != nil || inspected.Tags != expectedTags || inspected.Quality.Channels != fixtures[index].signal.channels {
			t.Fatalf("%s: inspected = %+v err = %v", path, inspected, err)
		}
		if (inspected.Quality.Duration - 2*time.Second).Abs() > 50*time.Millisecond {
			t.Fatalf("%s: duration = %v", path, inspected.Quality.Duration)
		}
	}
	first, _ := inspector.Inspect(context.Background(), paths[0])
	if first.Codec != "flac" || first.Quality.SampleRate != 96000 || first.Quality.BitDepth != 24 || first.Cover.Stream != 1 {
		t.Fatalf("flac = %+v", first)
	}
}

func TestFFprobeVerifier_Integration(t *testing.T) {
	_, ffprobe := requireFFmpeg(t)
	tags := audioFixtureTags{title: "Song", artist: "Artist", track: 1, trackTotal: 2}
	paths := buildAudioFixtures(t, t.TempDir(), []audioFixture{{relativePath: "a.m4a", format: "m4a", tags: tags, signal: audioFixtureSignal{sampleRate: 44100, channels: 2, seconds: 3}}})
	verifier := NewFFprobeVerifier(NewFFprobeInspector(NewProcessRunner(), ffprobe))
	metadata := domain.TrackMetadata{Title: "Song", Artist: "Artist", Track: "1/2"}

	matchErr := verifier.Verify(context.Background(), paths[0], services.PublishedAudio{Metadata: metadata, Duration: 3 * time.Second})
	mismatchErr := verifier.Verify(context.Background(), paths[0], services.PublishedAudio{Metadata: domain.TrackMetadata{Title: "Other"}, Duration: 3 * time.Second})

	if matchErr != nil || !errors.Is(mismatchErr, ErrPublishedAudioMismatch) {
		t.Fatalf("match = %v mismatch = %v", matchErr, mismatchErr)
	}
}
