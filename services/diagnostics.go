package services

import (
	"context"
	"fmt"
)

const (
	MinimumFreeBytes   uint64 = 2 << 30
	CheckFFmpeg               = "ffmpeg"
	CheckFDK                  = "fdk-aac"
	CheckSoxr                 = "soxr"
	CheckLastFM               = "last.fm"
	CheckOutput               = "output"
	CheckFreeSpace            = "free space"
	advisorCheckPrefix        = "advisor "
	fixFFmpeg                 = "winget install -e --id Gyan.FFmpeg"
	fixFDK                    = "install fre:ac or run: music-ops config set encoder.fdk_library <path to FDK-AAC.dll>"
	fixLastFM                 = "music-ops config set lastfm.api_key <key>"
	fixAdvisor                = "music-ops advisor install"
	fixOutput                 = "music-ops config set output.dir <path>"
	fixFreeSpace              = "free at least 2 GiB on the output drive"
	detailFDKFallback         = "FDK-AAC not found; the native AAC encoder at 512 kb/s is used"
	detailSoxrMissing         = "ffmpeg has no soxr resampler; the default resampler is used"
	detailNotWritable         = "output folder is not writable"
	bytesPerMebibyte          = 1 << 20
)

type EncoderReport struct {
	FFmpegVersion string
	HasFDK        bool
	HasSoxr       bool
}

type AdvisorReport struct {
	Name        string
	IsAvailable bool
	Detail      string
}

type OutputReport struct {
	Path       string
	IsWritable bool
	FreeBytes  uint64
}

type EncoderProbe interface {
	ProbeEncoder(ctx context.Context) (EncoderReport, error)
}

type TagSourceProbe interface {
	ProbeTagSource(ctx context.Context) error
}

type AdvisorProbe interface {
	ProbeAdvisors(ctx context.Context) []AdvisorReport
}

type OutputProbe interface {
	ProbeOutput(ctx context.Context) (OutputReport, error)
}

type CheckResult struct {
	Name    string
	IsOK    bool
	IsFatal bool
	Detail  string
	Fix     string
}

type DiagnosticsOutput struct {
	Checks    []CheckResult
	IsHealthy bool
}

type DiagnosticsService struct {
	encoder  EncoderProbe
	tags     TagSourceProbe
	advisors AdvisorProbe
	output   OutputProbe
}

func NewDiagnosticsService(encoder EncoderProbe, tags TagSourceProbe, advisors AdvisorProbe, output OutputProbe) DiagnosticsService {
	return DiagnosticsService{encoder: encoder, tags: tags, advisors: advisors, output: output}
}

func (service DiagnosticsService) Check(ctx context.Context) DiagnosticsOutput {
	checks := service.encoderChecks(ctx)
	checks = append(checks, service.tagSourceCheck(ctx))
	checks = append(checks, service.advisorChecks(ctx)...)
	checks = append(checks, service.outputChecks(ctx)...)
	return DiagnosticsOutput{Checks: checks, IsHealthy: areFatalChecksOK(checks)}
}

func (service DiagnosticsService) encoderChecks(ctx context.Context) []CheckResult {
	report, err := service.encoder.ProbeEncoder(ctx)
	ffmpeg := CheckResult{Name: CheckFFmpeg, IsOK: err == nil, IsFatal: true, Detail: report.FFmpegVersion, Fix: fixFFmpeg}
	if err != nil {
		ffmpeg.Detail = err.Error()
	}
	return []CheckResult{
		ffmpeg,
		optionalCheck(CheckFDK, report.HasFDK, detailFDKFallback, fixFDK),
		optionalCheck(CheckSoxr, report.HasSoxr, detailSoxrMissing, fixFFmpeg),
	}
}

func optionalCheck(name string, isOK bool, missingDetail, fix string) CheckResult {
	if isOK {
		return CheckResult{Name: name, IsOK: true}
	}
	return CheckResult{Name: name, Detail: missingDetail, Fix: fix}
}

func (service DiagnosticsService) tagSourceCheck(ctx context.Context) CheckResult {
	if err := service.tags.ProbeTagSource(ctx); err != nil {
		return CheckResult{Name: CheckLastFM, IsFatal: true, Detail: err.Error(), Fix: fixLastFM}
	}
	return CheckResult{Name: CheckLastFM, IsOK: true, IsFatal: true}
}

func (service DiagnosticsService) advisorChecks(ctx context.Context) []CheckResult {
	reports := service.advisors.ProbeAdvisors(ctx)
	checks := make([]CheckResult, 0, len(reports))
	for _, report := range reports {
		check := CheckResult{Name: advisorCheckPrefix + report.Name, IsOK: report.IsAvailable, Detail: report.Detail}
		if !report.IsAvailable {
			check.Fix = fixAdvisor
		}
		checks = append(checks, check)
	}
	return checks
}

func (service DiagnosticsService) outputChecks(ctx context.Context) []CheckResult {
	report, err := service.output.ProbeOutput(ctx)
	if err != nil {
		return []CheckResult{{Name: CheckOutput, IsFatal: true, Detail: err.Error(), Fix: fixOutput}}
	}
	output := CheckResult{Name: CheckOutput, IsOK: report.IsWritable, IsFatal: true, Detail: report.Path}
	if !report.IsWritable {
		output.Detail = detailNotWritable + ": " + report.Path
		output.Fix = fixOutput
	}
	space := CheckResult{Name: CheckFreeSpace, IsOK: report.FreeBytes >= MinimumFreeBytes, IsFatal: true, Detail: fmt.Sprintf("%d MiB free", report.FreeBytes/bytesPerMebibyte)}
	if !space.IsOK {
		space.Fix = fixFreeSpace
	}
	return []CheckResult{output, space}
}

func areFatalChecksOK(checks []CheckResult) bool {
	for _, check := range checks {
		if check.IsFatal && !check.IsOK {
			return false
		}
	}
	return true
}
