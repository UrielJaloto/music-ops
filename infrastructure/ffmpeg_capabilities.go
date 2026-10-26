package infrastructure

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
)

const (
	soxrBuildFlag      = "--enable-libsoxr"
	versionFieldIndex  = 2
	unknownFFmpegBuild = "unknown"
)

type ffmpegResampler interface {
	filter(targetRate int) string
	hasSoxr() bool
}

type soxrResampler struct{}

func (soxrResampler) filter(targetRate int) string {
	return "aresample=" + strconv.Itoa(targetRate) + ":resampler=soxr:precision=28"
}

func (soxrResampler) hasSoxr() bool {
	return true
}

type swrResampler struct{}

func (swrResampler) filter(targetRate int) string {
	return "aresample=" + strconv.Itoa(targetRate) + ":filter_size=64:phase_shift=10:cutoff=0.97"
}

func (swrResampler) hasSoxr() bool {
	return false
}

type FFmpegCapabilities struct {
	Version   string
	resampler ffmpegResampler
}

func DetectFFmpegCapabilities(ctx context.Context, runner ProcessExecutor, executable string) (FFmpegCapabilities, error) {
	version, err := runForOutput(ctx, runner, executable, "-version")
	if err != nil {
		return FFmpegCapabilities{}, err
	}
	buildConfiguration, err := runForOutput(ctx, runner, executable, "-hide_banner", "-buildconf")
	if err != nil {
		return FFmpegCapabilities{}, err
	}
	return parseFFmpegCapabilities(version, buildConfiguration), nil
}

func (capabilities FFmpegCapabilities) ResampleFilter(targetRate int) string {
	return capabilities.resamplerOrDefault().filter(targetRate)
}

func (capabilities FFmpegCapabilities) HasSoxr() bool {
	return capabilities.resamplerOrDefault().hasSoxr()
}

func (capabilities FFmpegCapabilities) resamplerOrDefault() ffmpegResampler {
	if capabilities.resampler == nil {
		return swrResampler{}
	}
	return capabilities.resampler
}

func parseFFmpegCapabilities(version, buildConfiguration string) FFmpegCapabilities {
	capabilities := FFmpegCapabilities{Version: parseFFmpegVersion(version), resampler: swrResampler{}}
	if strings.Contains(buildConfiguration, soxrBuildFlag) {
		capabilities.resampler = soxrResampler{}
	}
	return capabilities
}

func parseFFmpegVersion(output string) string {
	firstLine, _, _ := strings.Cut(output, "\n")
	fields := strings.Fields(firstLine)
	if len(fields) <= versionFieldIndex {
		return unknownFFmpegBuild
	}
	return fields[versionFieldIndex]
}

func runForOutput(ctx context.Context, runner ProcessExecutor, executable string, arguments ...string) (string, error) {
	var stdout bytes.Buffer
	if _, err := runner.Run(ctx, ProcessRequest{Executable: executable, Arguments: arguments, Stdout: &stdout}); err != nil {
		return "", fmt.Errorf("running %s %s: %w", executable, strings.Join(arguments, " "), err)
	}
	return stdout.String(), nil
}
