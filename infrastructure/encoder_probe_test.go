package infrastructure

import (
	"context"
	"errors"
	"testing"

	"github.com/UrielJaloto/music-ops/services"
)

type capabilityDetectorStub struct {
	capabilities FFmpegCapabilities
	err          error
}

func (stub capabilityDetectorStub) Detect(context.Context) (FFmpegCapabilities, error) {
	return stub.capabilities, stub.err
}

type fdkAvailabilityStub struct{ err error }

func (stub fdkAvailabilityStub) Available() error { return stub.err }

func TestEncoderProbe_ProbeEncoder(t *testing.T) {
	cases := []struct {
		name         string
		capabilities FFmpegCapabilities
		fdkErr       error
		expected     services.EncoderReport
	}{
		{name: "FDKAndSoxr", capabilities: FFmpegCapabilities{Version: "9.0", resampler: soxrResampler{}}, expected: services.EncoderReport{FFmpegVersion: "9.0", HasFDK: true, HasSoxr: true}},
		{name: "NoFDK", capabilities: FFmpegCapabilities{Version: "9.0", resampler: soxrResampler{}}, fdkErr: ErrFDKUnavailable, expected: services.EncoderReport{FFmpegVersion: "9.0", HasSoxr: true}},
		{name: "NoSoxr", capabilities: FFmpegCapabilities{Version: "6.0", resampler: swrResampler{}}, expected: services.EncoderReport{FFmpegVersion: "6.0", HasFDK: true}},
		{name: "Neither", capabilities: FFmpegCapabilities{Version: "6.0"}, fdkErr: ErrFDKUnavailable, expected: services.EncoderReport{FFmpegVersion: "6.0"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			probe := NewEncoderProbe(capabilityDetectorStub{capabilities: testCase.capabilities}, fdkAvailabilityStub{err: testCase.fdkErr})

			report, err := probe.ProbeEncoder(context.Background())

			if err != nil || report != testCase.expected {
				t.Fatalf("report = %+v err = %v", report, err)
			}
		})
	}
}

func TestEncoderProbe_FFmpegMissing(t *testing.T) {
	errMissing := errors.New("ffmpeg missing")
	probe := NewEncoderProbe(capabilityDetectorStub{err: errMissing}, fdkAvailabilityStub{})

	report, err := probe.ProbeEncoder(context.Background())

	if !errors.Is(err, errMissing) || report != (services.EncoderReport{HasFDK: true}) {
		t.Fatalf("report = %+v err = %v", report, err)
	}
}

func TestFFmpegCapabilityDetector_Detect(t *testing.T) {
	runner := &scriptedRunner{outputs: map[string]string{"-version": "ffmpeg version 7.1 Copyright", "-hide_banner -buildconf": "--enable-libsoxr"}}

	capabilities, err := NewFFmpegCapabilityDetector(runner, "ffmpeg").Detect(context.Background())

	if err != nil || capabilities.Version != "7.1" || !capabilities.HasSoxr() {
		t.Fatalf("capabilities = %+v err = %v", capabilities, err)
	}
}
