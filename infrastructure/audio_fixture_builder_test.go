//go:build integration

package infrastructure

import (
	"os"
	"strconv"
	"testing"
)

func firstAudioStream(t *testing.T, probed probedFile) probedStream {
	t.Helper()
	for _, stream := range probed.Streams {
		if stream.CodecType == "audio" {
			return stream
		}
	}
	t.Fatalf("no audio stream in %+v", probed)
	return probedStream{}
}

func hasCoverStream(probed probedFile) bool {
	for _, stream := range probed.Streams {
		if stream.CodecType == "video" {
			return true
		}
	}
	return false
}

func TestBuildAudioFixtures_Formats(t *testing.T) {
	tags := audioFixtureTags{title: "Song (feat. Guest)", artist: "Main", albumArtist: "Main", album: "Album", date: "2024", track: 3, trackTotal: 10, disc: 1}
	cases := []struct {
		fixture       audioFixture
		codec         string
		sampleRate    string
		bitsPerSample string
	}{
		{fixture: audioFixture{relativePath: "a/one.flac", format: "flac", tags: tags, signal: audioFixtureSignal{sampleRate: 96000, channels: 2, bitDepth: 24, seconds: 1}, hasCover: true}, codec: "flac", sampleRate: "96000", bitsPerSample: "24"},
		{fixture: audioFixture{relativePath: "b/two.wav", format: "wav", tags: tags, signal: audioFixtureSignal{sampleRate: 44100, channels: 1, bitDepth: 16, seconds: 1}}, codec: "pcm_s16le", sampleRate: "44100"},
		{fixture: audioFixture{relativePath: "three.mp3", format: "mp3", tags: tags, signal: audioFixtureSignal{sampleRate: 48000, channels: 2, seconds: 1}, hasCover: true}, codec: "mp3", sampleRate: "48000"},
		{fixture: audioFixture{relativePath: "four.m4a", format: "m4a", tags: tags, signal: audioFixtureSignal{sampleRate: 44100, channels: 2, seconds: 1}}, codec: "aac", sampleRate: "44100"},
		{fixture: audioFixture{relativePath: "five.opus", format: "opus", tags: tags, signal: audioFixtureSignal{sampleRate: 44100, channels: 2, seconds: 1}}, codec: "opus", sampleRate: "48000"},
	}
	for _, testCase := range cases {
		t.Run(testCase.fixture.format, func(t *testing.T) {
			paths := buildAudioFixtures(t, t.TempDir(), []audioFixture{testCase.fixture})

			probed := probeFixture(t, paths[0])
			stream := firstAudioStream(t, probed)
			if stream.CodecName != testCase.codec || stream.SampleRate != testCase.sampleRate || stream.Channels != testCase.fixture.signal.channels {
				t.Fatalf("stream = %+v", stream)
			}
			if testCase.bitsPerSample != "" && stream.BitsPerSample != testCase.bitsPerSample {
				t.Fatalf("bits = %s", stream.BitsPerSample)
			}
			if hasCoverStream(probed) != testCase.fixture.hasCover {
				t.Fatalf("cover = %v", hasCoverStream(probed))
			}
			assertFixtureTags(t, probed, tags)
		})
	}
}

func assertFixtureTags(t *testing.T, probed probedFile, expected audioFixtureTags) {
	t.Helper()
	tags := probed.allTags()
	if tags["title"] != expected.title || tags["artist"] != expected.artist || tags["album"] != expected.album {
		t.Fatalf("tags = %v", tags)
	}
	if tags["track"] != strconv.Itoa(expected.track)+"/"+strconv.Itoa(expected.trackTotal) && tags["track"] != strconv.Itoa(expected.track) {
		t.Fatalf("track tag = %q", tags["track"])
	}
}

func TestBuildAudioFixtures_Corrupt(t *testing.T) {
	fixture := audioFixture{relativePath: "bad.flac", format: "flac", signal: audioFixtureSignal{sampleRate: 44100, channels: 2, seconds: 2}, isCorrupt: true}

	paths := buildAudioFixtures(t, t.TempDir(), []audioFixture{fixture})

	info, err := os.Stat(paths[0])
	if err != nil || info.Size() != fixtureCorruptSize {
		t.Fatalf("size = %d err = %v", info.Size(), err)
	}
}
