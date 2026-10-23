package services

import (
	"context"
	"testing"
)

type encoderProbeStub struct {
	report EncoderReport
	err    error
}

func (stub encoderProbeStub) ProbeEncoder(context.Context) (EncoderReport, error) {
	return stub.report, stub.err
}

type tagProbeStub struct {
	err error
}

func (stub tagProbeStub) ProbeTagSource(context.Context) error {
	return stub.err
}

type advisorProbeStub struct {
	reports []AdvisorReport
}

func (stub advisorProbeStub) ProbeAdvisors(context.Context) []AdvisorReport {
	return stub.reports
}

type outputProbeStub struct {
	report OutputReport
	err    error
}

func (stub outputProbeStub) ProbeOutput(context.Context) (OutputReport, error) {
	return stub.report, stub.err
}

type diagnosticsDoubles struct {
	encoder  encoderProbeStub
	tags     tagProbeStub
	advisors advisorProbeStub
	output   outputProbeStub
}

func healthyDiagnostics() diagnosticsDoubles {
	return diagnosticsDoubles{
		encoder:  encoderProbeStub{report: EncoderReport{FFmpegVersion: "9.0.2", HasFDK: true, HasSoxr: true}},
		advisors: advisorProbeStub{reports: []AdvisorReport{{Name: "llama", IsAvailable: true, Detail: "Qwen"}}},
		output:   outputProbeStub{report: OutputReport{Path: "D:/Music", IsWritable: true, FreeBytes: MinimumFreeBytes}},
	}
}

func checkByName(t *testing.T, output DiagnosticsOutput, name string) CheckResult {
	t.Helper()
	for _, check := range output.Checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("check %q missing in %+v", name, output.Checks)
	return CheckResult{}
}

func TestDiagnosticsService_Check(t *testing.T) {
	cases := []struct {
		name            string
		arrange         func(*diagnosticsDoubles)
		failingCheck    string
		expectedFix     string
		expectedHealthy bool
	}{
		{name: "AllPassing_IsHealthy", arrange: func(*diagnosticsDoubles) {}, expectedHealthy: true},
		{name: "FFmpegMissing_IsUnhealthy", arrange: func(d *diagnosticsDoubles) { d.encoder.err = errTechnical }, failingCheck: CheckFFmpeg, expectedFix: fixFFmpeg},
		{name: "FDKMissing_StaysHealthy", arrange: func(d *diagnosticsDoubles) { d.encoder.report.HasFDK = false }, failingCheck: CheckFDK, expectedFix: fixFDK, expectedHealthy: true},
		{name: "SoxrMissing_StaysHealthy", arrange: func(d *diagnosticsDoubles) { d.encoder.report.HasSoxr = false }, failingCheck: CheckSoxr, expectedFix: fixFFmpeg, expectedHealthy: true},
		{name: "LastFMFailing_IsUnhealthy", arrange: func(d *diagnosticsDoubles) { d.tags.err = errTechnical }, failingCheck: CheckLastFM, expectedFix: fixLastFM},
		{
			name:         "AdvisorMissing_StaysHealthy",
			arrange:      func(d *diagnosticsDoubles) { d.advisors.reports[0].IsAvailable = false },
			failingCheck: "advisor llama", expectedFix: fixAdvisor, expectedHealthy: true,
		},
		{name: "OutputProbeError_IsUnhealthy", arrange: func(d *diagnosticsDoubles) { d.output.err = errTechnical }, failingCheck: CheckOutput, expectedFix: fixOutput},
		{name: "OutputNotWritable_IsUnhealthy", arrange: func(d *diagnosticsDoubles) { d.output.report.IsWritable = false }, failingCheck: CheckOutput, expectedFix: fixOutput},
		{name: "FreeSpaceBelowMinimum_IsUnhealthy", arrange: func(d *diagnosticsDoubles) { d.output.report.FreeBytes = MinimumFreeBytes - 1 }, failingCheck: CheckFreeSpace, expectedFix: fixFreeSpace},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			doubles := healthyDiagnostics()
			doubles.advisors.reports = append([]AdvisorReport{}, doubles.advisors.reports...)
			testCase.arrange(&doubles)
			service := NewDiagnosticsService(doubles.encoder, doubles.tags, doubles.advisors, doubles.output)

			output := service.Check(context.Background())

			if output.IsHealthy != testCase.expectedHealthy {
				t.Fatalf("healthy = %v checks = %+v", output.IsHealthy, output.Checks)
			}
			assertOnlyFailing(t, output, testCase.failingCheck, testCase.expectedFix)
		})
	}
}

func assertOnlyFailing(t *testing.T, output DiagnosticsOutput, failing, fix string) {
	t.Helper()
	for _, check := range output.Checks {
		isExpectedFailure := check.Name == failing
		if check.IsOK == isExpectedFailure {
			t.Fatalf("check %+v, expected failing %q", check, failing)
		}
		if isExpectedFailure && check.Fix != fix {
			t.Fatalf("fix = %q, want %q", check.Fix, fix)
		}
	}
}

func TestDiagnosticsService_Details(t *testing.T) {
	doubles := healthyDiagnostics()
	doubles.encoder.report.HasFDK = false
	service := NewDiagnosticsService(doubles.encoder, doubles.tags, doubles.advisors, doubles.output)

	output := service.Check(context.Background())

	if checkByName(t, output, CheckFFmpeg).Detail != "9.0.2" || checkByName(t, output, CheckFDK).Detail != detailFDKFallback {
		t.Fatalf("checks = %+v", output.Checks)
	}
	if checkByName(t, output, CheckFreeSpace).Detail != "2048 MiB free" || checkByName(t, output, "advisor llama").Detail != "Qwen" {
		t.Fatalf("checks = %+v", output.Checks)
	}
	if len(output.Checks) != 7 {
		t.Fatalf("checks = %d", len(output.Checks))
	}
}
