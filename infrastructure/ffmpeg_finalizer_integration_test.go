//go:build integration

package infrastructure

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
)

func audioMD5(t *testing.T, ffmpeg, path string) string {
	t.Helper()
	output, err := exec.CommandContext(context.Background(), ffmpeg, "-v", "error", "-i", path, "-map", "0:a", "-c", "copy", "-f", "md5", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}

func TestFFmpegFinalizer_Integration(t *testing.T) {
	ffmpeg, _ := requireFFmpeg(t)
	folder := t.TempDir()
	staged := filepath.Join(folder, "track.audio.m4a")
	encodeSineToMP4(t, staged, 2.0)
	coverSource := buildAudioFixtures(t, folder, []audioFixture{{relativePath: "src.flac", format: "flac", signal: audioFixtureSignal{sampleRate: 44100, channels: 2, seconds: 1}, hasCover: true}})[0]
	destination := filepath.Join(folder, "track.final.m4a")
	metadata := domain.TrackMetadata{Title: "Benção 🎵", Artist: "Motörhead", AlbumArtist: "¥$", Album: "$werve", Genre: "Rock", Track: "3/12", Disc: "1/2", Date: "2024", Composer: "Writer", Copyright: "Label"}
	request := services.FinalizeRequest{Staged: staged, CoverSource: coverSource, Cover: domain.CoverArt{Stream: 1, Codec: "png"}, Destination: destination, Metadata: metadata}

	err := NewFFmpegFinalizer(NewProcessRunner(), ffmpeg).Finalize(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}

	before, after := probeFixture(t, staged), probeFixture(t, destination)
	tags := after.allTags()
	expected := map[string]string{"title": metadata.Title, "artist": metadata.Artist, "album_artist": metadata.AlbumArtist, "album": metadata.Album, "genre": "Rock", "track": "3/12", "disc": "1/2", "date": "2024", "composer": "Writer", "copyright": "Label"}
	for key, value := range expected {
		if tags[key] != value {
			t.Errorf("tag %s = %q, want %q", key, tags[key], value)
		}
	}
	if !hasCoverStream(after) {
		t.Fatal("cover is not an attached picture")
	}
	if audioMD5(t, ffmpeg, staged) != audioMD5(t, ffmpeg, destination) {
		t.Fatal("audio packets changed")
	}
	if diff := (secondsToDuration(before.Format.Duration) - secondsToDuration(after.Format.Duration)).Abs(); diff > 1000000 {
		t.Fatalf("duration changed by %v", diff)
	}
}
