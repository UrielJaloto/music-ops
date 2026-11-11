package transcoding

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/internal/services/conversion"
	"github.com/UrielJaloto/music-ops/internal/services/diagnostics"
	"github.com/UrielJaloto/music-ops/internal/services/discovery"
	"github.com/UrielJaloto/music-ops/internal/services/publishing"
)

type fixedTools Tools

func (tools fixedTools) ResolveTools(context.Context) (Tools, error) {
	return Tools(tools), nil
}

type failingTools struct{ err error }

func (tools failingTools) ResolveTools(context.Context) (Tools, error) {
	return Tools{}, tools.err
}

func installedTools(t *testing.T) (string, string) {
	t.Helper()
	folder := t.TempDir()
	return createExecutable(t, filepath.Join(folder, "ffmpeg"), time.Now()), createExecutable(t, filepath.Join(folder, "ffprobe"), time.Now())
}

func TestToolchain_ResolveTools_ResolvesOnce(t *testing.T) {
	ffmpeg, ffprobe := installedTools(t)
	runner := &scriptedRunner{outputs: map[string]string{"-version": "ffmpeg version 7.1 Copyright", "-hide_banner -buildconf": "--enable-libsoxr"}}
	toolchain := NewToolchain(ToolchainSettings{FFmpeg: ffmpeg, FFprobe: ffprobe, LookPath: lookPathFinding(nil)}, runner)

	first, firstErr := toolchain.ResolveTools(context.Background())
	second, secondErr := toolchain.ResolveTools(context.Background())

	if err := errors.Join(firstErr, secondErr); err != nil {
		t.Fatal(err)
	}
	if first.FFmpeg != ffmpeg || first.FFprobe != ffprobe || first.Capabilities.Version != "7.1" || !first.Capabilities.HasSoxr() {
		t.Fatalf("tools = %+v", first)
	}
	if second != first || len(runner.requests) != 2 || runner.requests[0].Executable != ffmpeg {
		t.Fatalf("second = %+v requests = %d", second, len(runner.requests))
	}
}

func TestToolchain_ResolveTools_Failures(t *testing.T) {
	ffmpeg, ffprobe := installedTools(t)
	missing := filepath.Join(t.TempDir(), "missing")
	errRun := errors.New("does not run")
	cases := []struct {
		name             string
		settings         ToolchainSettings
		runnerErr        error
		expectedError    error
		expectedRequests int
	}{
		{name: "FFmpegMissing", settings: ToolchainSettings{FFmpeg: missing, FFprobe: ffprobe}, expectedError: diagnostics.ErrFFmpegMissing},
		{name: "FFprobeMissing", settings: ToolchainSettings{FFmpeg: ffmpeg, FFprobe: missing}, expectedError: diagnostics.ErrFFmpegMissing},
		{name: "FFmpegDoesNotRun", settings: ToolchainSettings{FFmpeg: ffmpeg, FFprobe: ffprobe}, runnerErr: errRun, expectedError: errRun, expectedRequests: 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			runner := &scriptedRunner{err: testCase.runnerErr}
			testCase.settings.LookPath = lookPathFinding(nil)

			first, firstErr := NewToolchain(testCase.settings, runner).ResolveTools(context.Background())

			if !errors.Is(firstErr, testCase.expectedError) || first != (Tools{}) || len(runner.requests) != testCase.expectedRequests {
				t.Fatalf("tools = %+v err = %v requests = %d", first, firstErr, len(runner.requests))
			}
		})
	}
}

func TestToolchain_FailureIsKept(t *testing.T) {
	runner := &scriptedRunner{}
	toolchain := NewToolchain(ToolchainSettings{FFmpeg: filepath.Join(t.TempDir(), "missing"), LookPath: lookPathFinding(nil)}, runner)

	_, firstErr := toolchain.ResolveTools(context.Background())
	_, secondErr := toolchain.ResolveTools(context.Background())

	if !errors.Is(firstErr, diagnostics.ErrFFmpegMissing) || !errors.Is(secondErr, diagnostics.ErrFFmpegMissing) {
		t.Fatalf("errors = %v %v", firstErr, secondErr)
	}
}

func TestFFprobeInspector_ToolsUnavailable_IsARunError(t *testing.T) {
	_, err := NewFFprobeInspector(&scriptedRunner{}, failingTools{err: errors.New("tools missing")}).Inspect(context.Background(), "a.flac")

	if !errors.Is(err, discovery.ErrInspectorUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestAdapters_ToolsUnavailable_ReturnErrorWithoutRunning(t *testing.T) {
	errMissing := errors.New("tools missing")
	tools := failingTools{err: errMissing}
	runner := &scriptedRunner{}
	destination := filepath.Join(t.TempDir(), "out.m4a")
	cases := []struct {
		name string
		call func() error
	}{
		{name: "Decoder", call: func() error {
			return NewFFmpegDecoder(runner, tools).Stream(context.Background(), DecodeRequest{Source: "a.flac"}, io.Discard)
		}},
		{name: "NativeEncoder", call: func() error {
			return NewFFmpegAACEncoder(runner, tools).Encode(context.Background(), conversion.EncodeRequest{Source: "a.flac", Destination: destination})
		}},
		{name: "Copier", call: func() error {
			return NewFFmpegAudioCopier(runner, tools).CopyAudio(context.Background(), "a.m4a", destination)
		}},
		{name: "Finalizer", call: func() error {
			return NewFFmpegFinalizer(runner, tools).Finalize(context.Background(), publishing.FinalizeRequest{Staged: "a.m4a", Destination: destination})
		}},
		{name: "Inspector", call: func() error {
			_, err := NewFFprobeInspector(runner, tools).Inspect(context.Background(), "a.flac")
			return err
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.call()

			if !errors.Is(err, errMissing) || len(runner.requests) != 0 {
				t.Fatalf("err = %v requests = %d", err, len(runner.requests))
			}
		})
	}
}
