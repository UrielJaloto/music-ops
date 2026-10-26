package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/UrielJaloto/music-ops/services"
)

const bitsPerKilobit = 1000

type FFmpegAACEncoder struct {
	runner       ProcessExecutor
	executable   string
	capabilities FFmpegCapabilities
}

func NewFFmpegAACEncoder(runner ProcessExecutor, executable string, capabilities FFmpegCapabilities) FFmpegAACEncoder {
	return FFmpegAACEncoder{runner: runner, executable: executable, capabilities: capabilities}
}

func (encoder FFmpegAACEncoder) Encode(ctx context.Context, request services.EncodeRequest) error {
	arguments := nativeEncodeArguments(processPath(request.Source), encoder.filterArguments(request), request)
	return runToPartial(ctx, encoder.runner, encoder.executable, arguments, request.Destination)
}

func (encoder FFmpegAACEncoder) filterArguments(request services.EncodeRequest) []string {
	if !request.Profile.NeedsResampling(request.Quality) {
		return []string{}
	}
	return []string{"-af", encoder.capabilities.ResampleFilter(request.Profile.SampleRate())}
}

func nativeEncodeArguments(source string, filter []string, request services.EncodeRequest) []string {
	arguments := []string{"-hide_banner", "-nostdin", "-y", "-i", source, "-map", "0:a:0", "-vn", "-map_metadata", "-1"}
	arguments = append(arguments, filter...)
	return append(arguments,
		"-ac", strconv.Itoa(request.Profile.OutputChannels(request.Quality)),
		"-c:a", "aac", "-aac_coder", "twoloop",
		"-b:a", strconv.Itoa(request.Profile.Bitrate()/bitsPerKilobit)+"k",
		"-cutoff", strconv.Itoa(request.Profile.CutoffHz()),
		"-f", "mp4", "-movflags", "+faststart", processPath(request.Destination+partialSuffix),
	)
}

type FFmpegAudioCopier struct {
	runner     ProcessExecutor
	executable string
}

func NewFFmpegAudioCopier(runner ProcessExecutor, executable string) FFmpegAudioCopier {
	return FFmpegAudioCopier{runner: runner, executable: executable}
}

func (copier FFmpegAudioCopier) CopyAudio(ctx context.Context, source, destination string) error {
	return runToPartial(ctx, copier.runner, copier.executable, copyAudioArguments(processPath(source), destination), destination)
}

func copyAudioArguments(source, destination string) []string {
	return []string{
		"-hide_banner", "-nostdin", "-y", "-i", source, "-map", "0:a:0", "-vn", "-c:a", "copy",
		"-map_metadata", "-1", "-f", "mp4", "-movflags", "+faststart", processPath(destination + partialSuffix),
	}
}

func runToPartial(ctx context.Context, runner ProcessExecutor, executable string, arguments []string, destination string) error {
	partial := destination + partialSuffix
	if _, err := runner.Run(ctx, ProcessRequest{Executable: executable, Arguments: arguments}); err != nil {
		return errors.Join(fmt.Errorf("writing %s: %w", destination, err), removeIfPresent(partial))
	}
	if err := os.Rename(partial, destination); err != nil {
		return errors.Join(fmt.Errorf("renaming %s: %w", destination, err), removeIfPresent(partial))
	}
	return nil
}
