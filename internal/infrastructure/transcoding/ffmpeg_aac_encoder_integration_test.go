//go:build integration

package transcoding

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/internal/domain/audio"
	"github.com/UrielJaloto/music-ops/internal/infrastructure/process"
	"github.com/UrielJaloto/music-ops/internal/services/conversion"
)

func detectTestCapabilities(t *testing.T) (string, FFmpegCapabilities) {
	t.Helper()
	ffmpeg, _ := requireFFmpeg(t)
	capabilities, err := DetectFFmpegCapabilities(context.Background(), process.NewProcessRunner(), ffmpeg)
	if err != nil {
		t.Fatal(err)
	}
	return ffmpeg, capabilities
}

func TestFFmpegAACEncoder_Integration_FLACAt512k(t *testing.T) {
	ffmpeg, capabilities := detectTestCapabilities(t)
	folder := t.TempDir()
	source := buildAudioFixtures(t, folder, []audioFixture{{relativePath: "a.flac", format: "flac", signal: audioFixtureSignal{sampleRate: 48000, channels: 2, bitDepth: 24, seconds: 3}}})[0]
	destination := filepath.Join(folder, "out.audio.m4a")
	quality := inspectedQuality(t, source)

	err := NewFFmpegAACEncoder(process.NewProcessRunner(), ffmpeg, capabilities).Encode(context.Background(), conversion.EncodeRequest{Source: source, Destination: destination, Profile: audio.NewEncodingProfile(audio.EngineNative), Quality: quality})

	if err != nil {
		t.Fatal(err)
	}
	assertNativeAudio(t, destination, 2, quality.Duration())
}

func TestFFmpegAACEncoder_Integration_CorruptSource(t *testing.T) {
	ffmpeg, capabilities := detectTestCapabilities(t)
	folder := t.TempDir()
	source := buildAudioFixtures(t, folder, []audioFixture{{relativePath: "bad.flac", format: "flac", signal: audioFixtureSignal{sampleRate: 44100, channels: 2, seconds: 2}, isCorrupt: true}})[0]
	destination := filepath.Join(folder, "out.audio.m4a")
	quality, _ := audio.NewAudioQuality(audio.AudioQualityDetails{SampleRate: 44100, Channels: 2, Duration: 2 * time.Second})

	err := NewFFmpegAACEncoder(process.NewProcessRunner(), ffmpeg, capabilities).Encode(context.Background(), conversion.EncodeRequest{Source: source, Destination: destination, Profile: audio.NewEncodingProfile(audio.EngineNative), Quality: quality})

	var processError process.ProcessError
	if !errors.As(err, &processError) {
		t.Fatalf("err = %v", err)
	}
	assertNoEncoderOutput(t, destination)
}

func TestFFmpegAudioCopier_Integration_KeepsCodecAndBitrate(t *testing.T) {
	ffmpeg, _ := requireFFmpeg(t)
	folder := t.TempDir()
	source := buildAudioFixtures(t, folder, []audioFixture{{relativePath: "a.m4a", format: "m4a", tags: audioFixtureTags{title: "Old"}, signal: audioFixtureSignal{sampleRate: 44100, channels: 2, seconds: 3}}})[0]
	destination := filepath.Join(folder, "out.audio.m4a")

	err := NewFFmpegAudioCopier(process.NewProcessRunner(), ffmpeg).CopyAudio(context.Background(), source, destination)

	sourceStream := firstAudioStream(t, probeFixture(t, source))
	copied := probeFixture(t, destination)
	copiedStream := firstAudioStream(t, copied)
	sourceBitrate, _ := strconv.Atoi(sourceStream.BitRate)
	copiedBitrate, _ := strconv.Atoi(copiedStream.BitRate)
	if err != nil || copiedStream.CodecName != "aac" || !isBitrateWithinTolerance(copiedBitrate, sourceBitrate) || copied.allTags()["title"] != "" {
		t.Fatalf("copied = %+v source bitrate = %d err = %v", copiedStream, sourceBitrate, err)
	}
}

func TestAACEncoderSelector_Integration(t *testing.T) {
	requireFDKLibrary(t)
	ffmpeg, capabilities := detectTestCapabilities(t)
	native := NewFFmpegAACEncoder(process.NewProcessRunner(), ffmpeg, capabilities)
	decoder := NewFFmpegDecoder(process.NewProcessRunner(), ffmpeg, capabilities)
	cases := []struct {
		name        string
		library     string
		expected    audio.EncodingProfile
		assertAudio func(t *testing.T, path string, channels int, duration time.Duration)
	}{
		{name: "InvalidLibraryPath_Native", library: filepath.Join(t.TempDir(), "FDK-AAC.dll"), expected: audio.NewEncodingProfile(audio.EngineNative), assertAudio: assertNativeAudio},
		{name: "ValidLibraryPath_FDK", library: fdkLibraryPath(), expected: audio.NewEncodingProfile(audio.EngineFDK), assertAudio: func(t *testing.T, path string, channels int, duration time.Duration) {
			assertPublishableAudio(t, path, 256000, channels, duration)
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			folder := t.TempDir()
			source := buildAudioFixtures(t, folder, []audioFixture{{relativePath: "a.flac", format: "flac", signal: audioFixtureSignal{sampleRate: 48000, channels: 2, bitDepth: 24, seconds: 2}}})[0]
			destination := filepath.Join(folder, "out.audio.m4a")
			selector := NewAACEncoderSelector(NewFDKAACEncoder(decoder, testCase.library), native)

			err := selector.Encode(context.Background(), conversion.EncodeRequest{Source: source, Destination: destination, Profile: selector.SelectProfile(), Quality: inspectedQuality(t, source)})

			if err != nil || selector.SelectProfile() != testCase.expected {
				t.Fatalf("profile = %v err = %v", selector.SelectProfile(), err)
			}
			testCase.assertAudio(t, destination, 2, 2*time.Second)
		})
	}
}
