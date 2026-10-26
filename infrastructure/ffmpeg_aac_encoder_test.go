package infrastructure

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
)

type outputWritingRunner struct {
	err       error
	arguments []string
}

func (runner *outputWritingRunner) Run(_ context.Context, request ProcessRequest) (ProcessResult, error) {
	runner.arguments = request.Arguments
	if runner.err != nil {
		return ProcessResult{}, runner.err
	}
	return ProcessResult{}, os.WriteFile(request.Arguments[len(request.Arguments)-1], []byte("m4a"), 0o600)
}

func TestFFmpegAACEncoder_ArgumentsGolden(t *testing.T) {
	cases := []struct {
		name     string
		quality  [3]int
		expected func(source, partial string) []string
	}{
		{name: "Resampled96k", quality: [3]int{96000, 24, 2}, expected: func(source, partial string) []string {
			return []string{"-hide_banner", "-nostdin", "-y", "-i", source, "-map", "0:a:0", "-vn", "-map_metadata", "-1", "-af", "aresample=44100:resampler=soxr:precision=28", "-ac", "2", "-c:a", "aac", "-aac_coder", "twoloop", "-b:a", "512k", "-cutoff", "20000", "-f", "mp4", "-movflags", "+faststart", partial}
		}},
		{name: "Native44kMono", quality: [3]int{44100, 16, 1}, expected: func(source, partial string) []string {
			return []string{"-hide_banner", "-nostdin", "-y", "-i", source, "-map", "0:a:0", "-vn", "-map_metadata", "-1", "-ac", "1", "-c:a", "aac", "-aac_coder", "twoloop", "-b:a", "512k", "-cutoff", "20000", "-f", "mp4", "-movflags", "+faststart", partial}
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			runner := &outputWritingRunner{}
			destination := filepath.Join(t.TempDir(), "out.audio.m4a")
			request := services.EncodeRequest{Source: "in.flac", Destination: destination, Profile: domain.NewEncodingProfile(domain.EngineNative), Quality: qualityOf(t, testCase.quality[0], testCase.quality[1], testCase.quality[2])}

			err := NewFFmpegAACEncoder(runner, "ffmpeg", FFmpegCapabilities{resampler: soxrResampler{}}).Encode(context.Background(), request)

			if diff := cmp.Diff(testCase.expected("in.flac", destination+".tmp"), runner.arguments); diff != "" || err != nil {
				t.Fatalf("arguments (-want +got):\n%s err = %v", diff, err)
			}
			if !isFile(destination) || isFile(destination+".tmp") {
				t.Fatal("partial file not renamed")
			}
		})
	}
}

func TestFFmpegAudioCopier_ArgumentsGolden(t *testing.T) {
	runner := &outputWritingRunner{}
	destination := filepath.Join(t.TempDir(), "out.audio.m4a")

	err := NewFFmpegAudioCopier(runner, "ffmpeg").CopyAudio(context.Background(), "in.m4a", destination)

	expected := []string{"-hide_banner", "-nostdin", "-y", "-i", "in.m4a", "-map", "0:a:0", "-vn", "-c:a", "copy", "-map_metadata", "-1", "-f", "mp4", "-movflags", "+faststart", destination + ".tmp"}
	if diff := cmp.Diff(expected, runner.arguments); diff != "" || err != nil || !isFile(destination) {
		t.Fatalf("arguments (-want +got):\n%s err = %v", diff, err)
	}
}

func TestRunToPartial_ProcessFailureRemovesPartial(t *testing.T) {
	errProcess := errors.New("ffmpeg failed")
	destination := filepath.Join(t.TempDir(), "out.m4a")
	writeTestFile(t, destination+".tmp", "half")

	err := runToPartial(context.Background(), &outputWritingRunner{err: errProcess}, "ffmpeg", []string{destination + ".tmp"}, destination)

	if !errors.Is(err, errProcess) || isFile(destination+".tmp") || isFile(destination) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunToPartial_RenameFailure(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "missing", "out.m4a")

	err := runToPartial(context.Background(), &scriptedRunner{}, "ffmpeg", []string{}, destination)

	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v", err)
	}
}

type optionalEncoderDouble struct {
	availability error
	calls        int
	checks       int
}

func (double *optionalEncoderDouble) Available() error {
	double.checks++
	return double.availability
}

func (double *optionalEncoderDouble) Encode(context.Context, services.EncodeRequest) error {
	double.calls++
	return nil
}

func TestAACEncoderSelector(t *testing.T) {
	cases := []struct {
		name            string
		availability    error
		expectedProfile domain.EncodingProfile
		expectedFDK     int
		expectedNative  int
	}{
		{name: "FDKAvailable_UsesFDK", expectedProfile: domain.NewEncodingProfile(domain.EngineFDK), expectedFDK: 2},
		{name: "FDKMissing_UsesNative", availability: ErrFDKUnavailable, expectedProfile: domain.NewEncodingProfile(domain.EngineNative), expectedNative: 2},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fdk := &optionalEncoderDouble{availability: testCase.availability}
			native := &optionalEncoderDouble{}
			selector := NewAACEncoderSelector(fdk, native)

			profile := selector.Profile()
			firstErr := selector.Encode(context.Background(), services.EncodeRequest{})
			secondErr := selector.Encode(context.Background(), services.EncodeRequest{})

			if profile != testCase.expectedProfile || fdk.calls != testCase.expectedFDK || native.calls != testCase.expectedNative || fdk.checks != 1 || firstErr != nil || secondErr != nil {
				t.Fatalf("profile = %v fdk = %d native = %d checks = %d", profile, fdk.calls, native.calls, fdk.checks)
			}
		})
	}
}
