package transcoding

import (
	"context"

	"github.com/UrielJaloto/music-ops/internal/services/diagnostics"
)

type CapabilityDetector interface {
	Detect(ctx context.Context) (FFmpegCapabilities, error)
}

type FDKAvailability interface {
	Available() error
}

type FFmpegCapabilityDetector struct {
	runner     ProcessExecutor
	executable string
}

func NewFFmpegCapabilityDetector(runner ProcessExecutor, executable string) FFmpegCapabilityDetector {
	return FFmpegCapabilityDetector{runner: runner, executable: executable}
}

func (detector FFmpegCapabilityDetector) Detect(ctx context.Context) (FFmpegCapabilities, error) {
	return DetectFFmpegCapabilities(ctx, detector.runner, detector.executable)
}

type EncoderProbe struct {
	detector CapabilityDetector
	fdk      FDKAvailability
}

func NewEncoderProbe(detector CapabilityDetector, fdk FDKAvailability) EncoderProbe {
	return EncoderProbe{detector: detector, fdk: fdk}
}

func (probe EncoderProbe) ProbeEncoder(ctx context.Context) (diagnostics.EncoderReport, error) {
	capabilities, err := probe.detector.Detect(ctx)
	if err != nil {
		return diagnostics.EncoderReport{HasFDK: probe.fdk.Available() == nil}, err
	}
	return diagnostics.EncoderReport{
		FFmpegVersion: capabilities.Version,
		HasFDK:        probe.fdk.Available() == nil,
		HasSoxr:       capabilities.HasSoxr(),
	}, nil
}
