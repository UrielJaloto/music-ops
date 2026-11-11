package transcoding

import (
	"context"
	"io"
	"strconv"

	"github.com/UrielJaloto/music-ops/internal/domain/audio"
	"github.com/UrielJaloto/music-ops/internal/infrastructure/process"
)

const (
	pcmOutputFormat = "osf=s16:dither_method=triangular_hp"
	pcmFormatOnly   = "aresample=" + pcmOutputFormat
)

type DecodeRequest struct {
	Source  string
	Profile audio.EncodingProfile
	Quality audio.AudioQuality
}

type FFmpegDecoder struct {
	runner ProcessExecutor
	tools  ToolResolver
}

func NewFFmpegDecoder(runner ProcessExecutor, tools ToolResolver) FFmpegDecoder {
	return FFmpegDecoder{runner: runner, tools: tools}
}

func (decoder FFmpegDecoder) Stream(ctx context.Context, request DecodeRequest, sink io.Writer) error {
	tools, err := decoder.tools.ResolveTools(ctx)
	if err != nil {
		return err
	}
	arguments := buildDecodeArguments(toProcessPath(request.Source), tools.Capabilities.buildPCMFilter(request), request.Profile.CountOutputChannels(request.Quality))
	_, err = decoder.runner.Run(ctx, process.ProcessRequest{Executable: tools.FFmpeg, Arguments: arguments, Stdout: sink})
	return err
}

func (capabilities FFmpegCapabilities) buildPCMFilter(request DecodeRequest) string {
	if !request.Profile.NeedsResampling(request.Quality) {
		return pcmFormatOnly
	}
	return capabilities.BuildResampleFilter(request.Profile.SampleRate()) + ":" + pcmOutputFormat
}

func buildDecodeArguments(source, filter string, channels int) []string {
	return []string{
		"-hide_banner", "-nostdin", "-i", source, "-map", "0:a:0", "-vn",
		"-af", filter, "-ac", strconv.Itoa(channels), "-f", "s16le", "-",
	}
}
