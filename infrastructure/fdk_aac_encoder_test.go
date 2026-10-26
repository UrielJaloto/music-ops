//go:build integration

package infrastructure

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	mp4 "github.com/abema/go-mp4"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
)

func fdkLibraryPath() string {
	path, isSet := os.LookupEnv("MUSIC_OPS_ENCODER_FDK_LIBRARY")
	if !isSet || path == "" {
		return defaultFDKLibraryPath
	}
	return path
}

func newTestFDKEncoder(t *testing.T) FDKAACEncoder {
	t.Helper()
	requireFDKLibrary(t)
	ffmpeg, _ := requireFFmpeg(t)
	capabilities, err := DetectFFmpegCapabilities(context.Background(), NewProcessRunner(), ffmpeg)
	if err != nil {
		t.Fatal(err)
	}
	return NewFDKAACEncoder(NewFFmpegDecoder(NewProcessRunner(), ffmpeg, capabilities), fdkLibraryPath())
}

func inspectedQuality(t *testing.T, path string) domain.AudioQuality {
	t.Helper()
	_, ffprobe := requireFFmpeg(t)
	inspected, err := NewFFprobeInspector(NewProcessRunner(), ffprobe).Inspect(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	quality, err := domain.NewAudioQuality(inspected.Quality)
	if err != nil {
		t.Fatal(err)
	}
	return quality
}

func topLevelBoxes(t *testing.T, path string) []string {
	t.Helper()
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	boxes := []string{}
	_, err = mp4.ReadBoxStructure(file, func(handle *mp4.ReadHandle) (any, error) {
		boxes = append(boxes, handle.BoxInfo.Type.String())
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return boxes
}

func assertPublishableAudio(t *testing.T, path string, expectedBitrate, expectedChannels int, duration time.Duration) {
	t.Helper()
	boxes := topLevelBoxes(t, path)
	if len(boxes) < 3 || boxes[0] != "ftyp" || boxes[1] != "moov" || boxes[2] != "mdat" {
		t.Fatalf("boxes = %v, want ftyp moov mdat (fast start)", boxes)
	}
	probed := probeFixture(t, path)
	stream := firstAudioStream(t, probed)
	bitrate, _ := strconv.Atoi(stream.BitRate)
	if containerDuration := secondsToDuration(probed.Format.Duration); (containerDuration - duration).Abs() > 20*time.Millisecond {
		t.Fatalf("container duration = %v, want %v", containerDuration, duration)
	}
	if stream.CodecName != "aac" || stream.Profile != "LC" || stream.SampleRate != "44100" || stream.Channels != expectedChannels {
		t.Fatalf("stream = %+v", stream)
	}
	if !isBitrateWithinTolerance(bitrate, expectedBitrate) {
		t.Fatalf("bitrate = %d, want %d ± 8 %%", bitrate, expectedBitrate)
	}
	_, ffprobe := requireFFmpeg(t)
	verifier := NewFFprobeVerifier(NewFFprobeInspector(NewProcessRunner(), ffprobe))
	if err := verifier.Verify(context.Background(), path, services.PublishedAudio{Duration: duration, Bitrate: expectedBitrate, IsReencoded: true}); err != nil {
		t.Fatal(err)
	}
}

func TestFDKAACEncoder_Encode_Sources(t *testing.T) {
	encoder := newTestFDKEncoder(t)
	cases := []struct {
		fixture          audioFixture
		expectedChannels int
	}{
		{fixture: audioFixture{relativePath: "hires.flac", format: "flac", signal: audioFixtureSignal{sampleRate: 96000, channels: 2, bitDepth: 24, seconds: 3}}, expectedChannels: 2},
		{fixture: audioFixture{relativePath: "mono.wav", format: "wav", signal: audioFixtureSignal{sampleRate: 44100, channels: 1, bitDepth: 16, seconds: 3}}, expectedChannels: 1},
		{fixture: audioFixture{relativePath: "surround.flac", format: "flac", signal: audioFixtureSignal{sampleRate: 48000, channels: 6, bitDepth: 16, seconds: 3}}, expectedChannels: 2},
		{fixture: audioFixture{relativePath: "lossy.mp3", format: "mp3", signal: audioFixtureSignal{sampleRate: 44100, channels: 2, seconds: 3}}, expectedChannels: 2},
	}
	for _, testCase := range cases {
		t.Run(testCase.fixture.relativePath, func(t *testing.T) {
			folder := t.TempDir()
			source := buildAudioFixtures(t, folder, []audioFixture{testCase.fixture})[0]
			destination := filepath.Join(folder, "out.audio.m4a")
			request := services.EncodeRequest{Source: source, Destination: destination, Profile: domain.NewEncodingProfile(domain.EngineFDK), Quality: inspectedQuality(t, source)}

			err := encoder.Encode(context.Background(), request)

			if err != nil {
				t.Fatal(err)
			}
			assertPublishableAudio(t, destination, 256000, testCase.expectedChannels, inspectedQuality(t, source).Duration())
			if isFile(destination + partialSuffix) {
				t.Fatal("partial file left behind")
			}
		})
	}
}

func TestFDKAACEncoder_Encode_CorruptSource(t *testing.T) {
	encoder := newTestFDKEncoder(t)
	folder := t.TempDir()
	source := buildAudioFixtures(t, folder, []audioFixture{{relativePath: "bad.flac", format: "flac", signal: audioFixtureSignal{sampleRate: 44100, channels: 2, seconds: 2}, isCorrupt: true}})[0]
	destination := filepath.Join(folder, "out.audio.m4a")
	quality, _ := domain.NewAudioQuality(domain.AudioQualityDetails{SampleRate: 44100, Channels: 2, Duration: 2 * time.Second})

	err := encoder.Encode(context.Background(), services.EncodeRequest{Source: source, Destination: destination, Profile: domain.NewEncodingProfile(domain.EngineFDK), Quality: quality})

	var processError ProcessError
	if !errors.As(err, &processError) || processError.StderrTail == "" {
		t.Fatalf("err = %v", err)
	}
	assertNoEncoderOutput(t, destination)
}

func TestFDKAACEncoder_Encode_Cancelled(t *testing.T) {
	encoder := newTestFDKEncoder(t)
	folder := t.TempDir()
	source := buildAudioFixtures(t, folder, []audioFixture{{relativePath: "long.flac", format: "flac", signal: audioFixtureSignal{sampleRate: 44100, channels: 2, seconds: 30}}})[0]
	destination := filepath.Join(folder, "out.audio.m4a")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := encoder.Encode(ctx, services.EncodeRequest{Source: source, Destination: destination, Profile: domain.NewEncodingProfile(domain.EngineFDK), Quality: inspectedQuality(t, source)})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	assertNoEncoderOutput(t, destination)
}

func TestFDKAACEncoder_Encode_LibraryMissing(t *testing.T) {
	encoder := NewFDKAACEncoder(NewFFmpegDecoder(NewProcessRunner(), "ffmpeg", FFmpegCapabilities{}), filepath.Join(t.TempDir(), "FDK-AAC.dll"))

	err := encoder.Encode(context.Background(), services.EncodeRequest{Destination: filepath.Join(t.TempDir(), "x.m4a")})

	if !errors.Is(err, ErrFDKUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func assertNoEncoderOutput(t *testing.T, destination string) {
	t.Helper()
	matches, err := filepath.Glob(destination + "*")
	if err != nil || len(matches) != 0 {
		t.Fatalf("leftover files = %v err = %v", matches, err)
	}
}
