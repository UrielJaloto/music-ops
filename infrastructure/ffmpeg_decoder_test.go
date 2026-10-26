package infrastructure

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/UrielJaloto/music-ops/domain"
)

func capturedFFmpegOutput(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(filepath.Join("testdata", "ffmpeg", name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestParseFFmpegCapabilities_CapturedBuilds(t *testing.T) {
	cases := []struct {
		name            string
		version         string
		buildconf       string
		expectedVersion string
		expectedSoxr    bool
	}{
		{name: "GyanFullBuild", version: "gyan_version.txt", buildconf: "gyan_buildconf.txt", expectedVersion: "9.0.2-full_build-www.gyan.dev", expectedSoxr: true},
		{name: "FreacSmallBuild", version: "freac_version.txt", buildconf: "freac_buildconf.txt", expectedVersion: "6.0", expectedSoxr: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			capabilities := parseFFmpegCapabilities(capturedFFmpegOutput(t, testCase.version), capturedFFmpegOutput(t, testCase.buildconf))

			if capabilities.Version != testCase.expectedVersion || capabilities.HasSoxr() != testCase.expectedSoxr {
				t.Fatalf("capabilities = %+v soxr = %v", capabilities, capabilities.HasSoxr())
			}
		})
	}
}

func TestParseFFmpegVersion_Unexpected(t *testing.T) {
	if version := parseFFmpegVersion("garbage"); version != "unknown" {
		t.Fatalf("version = %q", version)
	}
}

func TestFFmpegCapabilities_ResampleFilter(t *testing.T) {
	soxr := FFmpegCapabilities{resampler: soxrResampler{}}
	swr := FFmpegCapabilities{resampler: swrResampler{}}

	if filter := soxr.ResampleFilter(44100); filter != "aresample=44100:resampler=soxr:precision=28" {
		t.Fatalf("soxr = %s", filter)
	}
	if filter := swr.ResampleFilter(44100); filter != "aresample=44100:filter_size=64:phase_shift=10:cutoff=0.97" {
		t.Fatalf("swr = %s", filter)
	}
	if (FFmpegCapabilities{}).ResampleFilter(44100) != swr.ResampleFilter(44100) || (FFmpegCapabilities{}).HasSoxr() {
		t.Fatal("zero capabilities must fall back to swr")
	}
}

type scriptedRunner struct {
	outputs  map[string]string
	err      error
	requests []ProcessRequest
}

func (runner *scriptedRunner) Run(_ context.Context, request ProcessRequest) (ProcessResult, error) {
	runner.requests = append(runner.requests, request)
	if runner.err != nil {
		return ProcessResult{}, runner.err
	}
	if request.Stdout != nil {
		_, _ = request.Stdout.Write([]byte(runner.outputs[strings.Join(request.Arguments, " ")]))
	}
	return ProcessResult{}, nil
}

func TestDetectFFmpegCapabilities(t *testing.T) {
	runner := &scriptedRunner{outputs: map[string]string{
		"-version":                capturedFFmpegOutput(t, "gyan_version.txt"),
		"-hide_banner -buildconf": capturedFFmpegOutput(t, "gyan_buildconf.txt"),
	}}

	capabilities, err := DetectFFmpegCapabilities(context.Background(), runner, "ffmpeg")

	if err != nil || !capabilities.HasSoxr() || capabilities.Version != "9.0.2-full_build-www.gyan.dev" {
		t.Fatalf("capabilities = %+v err = %v", capabilities, err)
	}
}

func TestDetectFFmpegCapabilities_ProcessFails(t *testing.T) {
	errMissing := errors.New("not found")

	_, err := DetectFFmpegCapabilities(context.Background(), &scriptedRunner{err: errMissing}, "ffmpeg")

	if !errors.Is(err, errMissing) {
		t.Fatalf("err = %v", err)
	}
}

func qualityOf(t *testing.T, sampleRate, bitDepth, channels int) domain.AudioQuality {
	t.Helper()
	quality, err := domain.NewAudioQuality(domain.AudioQualityDetails{SampleRate: sampleRate, BitDepth: bitDepth, Channels: channels, Duration: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return quality
}

func TestFFmpegDecoder_ArgumentsGolden(t *testing.T) {
	soxr := FFmpegCapabilities{resampler: soxrResampler{}}
	swr := FFmpegCapabilities{resampler: swrResampler{}}
	head := []string{"-hide_banner", "-nostdin", "-i", "in.flac", "-map", "0:a:0", "-vn", "-af"}
	cases := []struct {
		name         string
		capabilities FFmpegCapabilities
		quality      [3]int
		expectedTail []string
	}{
		{name: "Stereo44k_FormatOnly", capabilities: soxr, quality: [3]int{44100, 16, 2}, expectedTail: []string{"aresample=osf=s16:dither_method=triangular_hp", "-ac", "2"}},
		{name: "HiRes96k24bit_Soxr", capabilities: soxr, quality: [3]int{96000, 24, 2}, expectedTail: []string{"aresample=44100:resampler=soxr:precision=28:osf=s16:dither_method=triangular_hp", "-ac", "2"}},
		{name: "HiRes96k24bit_Swr", capabilities: swr, quality: [3]int{96000, 24, 2}, expectedTail: []string{"aresample=44100:filter_size=64:phase_shift=10:cutoff=0.97:osf=s16:dither_method=triangular_hp", "-ac", "2"}},
		{name: "Mono44k", capabilities: soxr, quality: [3]int{44100, 16, 1}, expectedTail: []string{"aresample=osf=s16:dither_method=triangular_hp", "-ac", "1"}},
		{name: "SixChannels48k_Downmixed", capabilities: swr, quality: [3]int{48000, 24, 6}, expectedTail: []string{"aresample=44100:filter_size=64:phase_shift=10:cutoff=0.97:osf=s16:dither_method=triangular_hp", "-ac", "2"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			runner := &scriptedRunner{}
			request := DecodeRequest{Source: "in.flac", Profile: domain.NewEncodingProfile(domain.EngineFDK), Quality: qualityOf(t, testCase.quality[0], testCase.quality[1], testCase.quality[2])}

			err := NewFFmpegDecoder(runner, "ffmpeg", testCase.capabilities).Stream(context.Background(), request, &strings.Builder{})

			expected := append(append(append([]string{}, head...), testCase.expectedTail...), "-f", "s16le", "-")
			if diff := cmp.Diff(expected, runner.requests[0].Arguments); diff != "" || err != nil {
				t.Fatalf("arguments (-want +got):\n%s err = %v", diff, err)
			}
		})
	}
}
