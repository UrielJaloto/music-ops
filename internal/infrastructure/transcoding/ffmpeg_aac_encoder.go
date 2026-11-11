package transcoding

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/UrielJaloto/music-ops/internal/infrastructure/process"
	"github.com/UrielJaloto/music-ops/internal/services/conversion"
)

const bitsPerKilobit = 1000

type FFmpegAACEncoder struct {
	runner ProcessExecutor
	tools  ToolResolver
}

func NewFFmpegAACEncoder(runner ProcessExecutor, tools ToolResolver) FFmpegAACEncoder {
	return FFmpegAACEncoder{runner: runner, tools: tools}
}

func (encoder FFmpegAACEncoder) Encode(ctx context.Context, request conversion.EncodeRequest) error {
	tools, err := encoder.tools.ResolveTools(ctx)
	if err != nil {
		return err
	}
	arguments := buildNativeEncodeArguments(toProcessPath(request.Source), tools.Capabilities.buildFilterArguments(request), request)
	return runToPartial(ctx, encoder.runner, process.ProcessRequest{Executable: tools.FFmpeg, Arguments: arguments}, request.Destination)
}

func (capabilities FFmpegCapabilities) buildFilterArguments(request conversion.EncodeRequest) []string {
	if !request.Profile.NeedsResampling(request.Quality) {
		return []string{}
	}
	return []string{"-af", capabilities.BuildResampleFilter(request.Profile.SampleRate())}
}

func buildNativeEncodeArguments(source string, filter []string, request conversion.EncodeRequest) []string {
	arguments := []string{"-hide_banner", "-nostdin", "-y", "-i", source, "-map", "0:a:0", "-vn", "-map_metadata", "-1"}
	arguments = append(arguments, filter...)
	return append(arguments,
		"-ac", strconv.Itoa(request.Profile.CountOutputChannels(request.Quality)),
		"-c:a", "aac", "-aac_coder", "twoloop",
		"-b:a", strconv.Itoa(request.Profile.Bitrate()/bitsPerKilobit)+"k",
		"-cutoff", strconv.Itoa(request.Profile.CutoffHz()),
		"-f", "mp4", "-movflags", "+faststart", toProcessPath(request.Destination+partialSuffix),
	)
}

type FFmpegAudioCopier struct {
	runner ProcessExecutor
	tools  ToolResolver
}

func NewFFmpegAudioCopier(runner ProcessExecutor, tools ToolResolver) FFmpegAudioCopier {
	return FFmpegAudioCopier{runner: runner, tools: tools}
}

func (copier FFmpegAudioCopier) CopyAudio(ctx context.Context, source, destination string) error {
	tools, err := copier.tools.ResolveTools(ctx)
	if err != nil {
		return err
	}
	return runToPartial(ctx, copier.runner, process.ProcessRequest{Executable: tools.FFmpeg, Arguments: buildCopyAudioArguments(toProcessPath(source), destination)}, destination)
}

func buildCopyAudioArguments(source, destination string) []string {
	return []string{
		"-hide_banner", "-nostdin", "-y", "-i", source, "-map", "0:a:0", "-vn", "-c:a", "copy",
		"-map_metadata", "-1", "-f", "mp4", "-movflags", "+faststart", toProcessPath(destination + partialSuffix),
	}
}

func runToPartial(ctx context.Context, runner ProcessExecutor, request process.ProcessRequest, destination string) error {
	partial := destination + partialSuffix
	if _, err := runner.Run(ctx, request); err != nil {
		return errors.Join(fmt.Errorf("writing %s: %w", destination, err), removeIfPresent(partial))
	}
	if err := os.Rename(partial, destination); err != nil {
		return errors.Join(fmt.Errorf("renaming %s: %w", destination, err), removeIfPresent(partial))
	}
	return nil
}
