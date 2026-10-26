package infrastructure

import (
	"context"
	"io"
	"strconv"

	"github.com/UrielJaloto/music-ops/domain"
)

const (
	pcmOutputFormat = "osf=s16:dither_method=triangular_hp"
	pcmFormatOnly   = "aresample=" + pcmOutputFormat
)

type DecodeRequest struct {
	Source  string
	Profile domain.EncodingProfile
	Quality domain.AudioQuality
}

type FFmpegDecoder struct {
	runner       ProcessExecutor
	executable   string
	capabilities FFmpegCapabilities
}

func NewFFmpegDecoder(runner ProcessExecutor, executable string, capabilities FFmpegCapabilities) FFmpegDecoder {
	return FFmpegDecoder{runner: runner, executable: executable, capabilities: capabilities}
}

func (decoder FFmpegDecoder) Stream(ctx context.Context, request DecodeRequest, sink io.Writer) error {
	arguments := decodeArguments(processPath(request.Source), decoder.pcmFilter(request), request.Profile.OutputChannels(request.Quality))
	_, err := decoder.runner.Run(ctx, ProcessRequest{Executable: decoder.executable, Arguments: arguments, Stdout: sink})
	return err
}

func (decoder FFmpegDecoder) pcmFilter(request DecodeRequest) string {
	if !request.Profile.NeedsResampling(request.Quality) {
		return pcmFormatOnly
	}
	return decoder.capabilities.ResampleFilter(request.Profile.SampleRate()) + ":" + pcmOutputFormat
}

func decodeArguments(source, filter string, channels int) []string {
	return []string{
		"-hide_banner", "-nostdin", "-i", source, "-map", "0:a:0", "-vn",
		"-af", filter, "-ac", strconv.Itoa(channels), "-f", "s16le", "-",
	}
}
