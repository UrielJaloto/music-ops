package transcoding

import (
	"context"

	"github.com/UrielJaloto/music-ops/internal/services/diagnostics"
)

type FDKAvailability interface {
	Available() error
}

type EncoderProbe struct {
	tools ToolResolver
	fdk   FDKAvailability
}

func NewEncoderProbe(tools ToolResolver, fdk FDKAvailability) EncoderProbe {
	return EncoderProbe{tools: tools, fdk: fdk}
}

func (probe EncoderProbe) ProbeEncoder(ctx context.Context) (diagnostics.EncoderReport, error) {
	tools, err := probe.tools.ResolveTools(ctx)
	if err != nil {
		return diagnostics.EncoderReport{HasFDK: probe.fdk.Available() == nil}, err
	}
	return diagnostics.EncoderReport{
		FFmpegVersion: tools.Capabilities.Version,
		HasFDK:        probe.fdk.Available() == nil,
		HasSoxr:       tools.Capabilities.HasSoxr(),
	}, nil
}
