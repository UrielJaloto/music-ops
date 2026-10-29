package presentation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/UrielJaloto/music-ops/services"
)

type settingsManagerFake struct {
	values map[string]string
	err    error
	sets   []services.SetSettingInput
}

func (fake *settingsManagerFake) Show(context.Context) (services.SettingsOutput, error) {
	return services.SettingsOutput{Location: "C:/AppData/music-ops/config.yaml", Values: []services.SettingValueOutput{
		{Key: "output.dir", Value: "E:/Music"}, {Key: "lastfm.api_key", Value: "****abcd"}, {Key: "advisor.port", Value: "8089"},
	}}, fake.err
}

func (fake *settingsManagerFake) Get(_ context.Context, key string) (services.SettingValueOutput, error) {
	return services.SettingValueOutput{Key: key, Value: fake.values[key]}, fake.err
}

func (fake *settingsManagerFake) Set(_ context.Context, input services.SetSettingInput) error {
	fake.sets = append(fake.sets, input)
	return fake.err
}

func (fake *settingsManagerFake) Path() services.SettingsLocationOutput {
	return services.SettingsLocationOutput{Path: "C:/AppData/music-ops/config.yaml"}
}

func runSettings(fake *settingsManagerFake, arguments ...string) (int, string) {
	var output bytes.Buffer
	controller := NewSettingsController(fake, NewSettingsPresenter(&output, NewErrorPresenter(&output)))
	code := controller.Execute(context.Background(), arguments)
	return code, output.String()
}

func TestSettingsController_Subcommands(t *testing.T) {
	cases := []struct {
		arguments    []string
		expectedCode int
		expectedText string
	}{
		{arguments: []string{"get", "output.dir"}, expectedCode: 0, expectedText: "E:/Music\n"},
		{arguments: []string{"set", "output.dir", "F:/Music"}, expectedCode: 0, expectedText: "output.dir set to F:/Music\n"},
		{arguments: []string{"path"}, expectedCode: 0, expectedText: "C:/AppData/music-ops/config.yaml\n"},
		{arguments: []string{}, expectedCode: 1, expectedText: "usage: music-ops config show|get <key>|set <key> <value>|path\n"},
		{arguments: []string{"remove"}, expectedCode: 1, expectedText: "usage: music-ops config show|get <key>|set <key> <value>|path\n"},
		{arguments: []string{"get"}, expectedCode: 1, expectedText: "usage: music-ops config get <key>\n"},
		{arguments: []string{"set", "output.dir"}, expectedCode: 1, expectedText: "usage: music-ops config set <key> <value>\n"},
	}
	for _, testCase := range cases {
		t.Run(fmt.Sprint(testCase.arguments), func(t *testing.T) {
			code, output := runSettings(&settingsManagerFake{values: map[string]string{"output.dir": "E:/Music"}}, testCase.arguments...)

			if code != testCase.expectedCode || output != testCase.expectedText {
				t.Fatalf("code = %d output = %q", code, output)
			}
		})
	}
}

func TestSettingsController_ShowGolden(t *testing.T) {
	code, output := runSettings(&settingsManagerFake{}, "show")

	if code != 0 || output != readGolden(t, "config_show.golden") {
		t.Fatalf("code = %d output:\n%s", code, output)
	}
}

func TestSettingsController_Errors(t *testing.T) {
	cases := [][]string{{"show"}, {"get", "output.color"}, {"set", "output.color", "x"}}
	for _, arguments := range cases {
		t.Run(fmt.Sprint(arguments), func(t *testing.T) {
			code, output := runSettings(&settingsManagerFake{err: services.ErrSettingUnknown}, arguments...)

			if code != 1 || output != "error: unknown setting; run: music-ops config show\n" {
				t.Fatalf("code = %d output = %q", code, output)
			}
		})
	}
}

type healthCheckerStub struct{ output services.DiagnosticsOutput }

func (stub healthCheckerStub) Check(context.Context) services.DiagnosticsOutput { return stub.output }

func TestDiagnosticsController(t *testing.T) {
	checks := []services.CheckResult{
		{Name: "ffmpeg", IsOK: true, IsFatal: true, Detail: "9.0.2"},
		{Name: "fdk-aac", IsOK: false, Detail: "FDK-AAC not found", Fix: "install fre:ac"},
		{Name: "output", IsOK: false, IsFatal: true, Detail: "output folder is not writable", Fix: "music-ops config set output.dir <path>"},
	}
	cases := []struct {
		name         string
		output       services.DiagnosticsOutput
		golden       string
		expectedCode int
	}{
		{name: "Healthy", output: services.DiagnosticsOutput{Checks: checks[:2], IsHealthy: true}, golden: "doctor_healthy.golden", expectedCode: 0},
		{name: "Unhealthy", output: services.DiagnosticsOutput{Checks: checks, IsHealthy: false}, golden: "doctor_unhealthy.golden", expectedCode: 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var output bytes.Buffer

			code := NewDiagnosticsController(healthCheckerStub{output: testCase.output}, NewDiagnosticsPresenter(&output)).Execute(context.Background(), nil)

			if code != testCase.expectedCode {
				t.Fatalf("code = %d", code)
			}
			assertGolden(t, testCase.golden, output.String())
		})
	}
}

type versionReporterStub struct{}

func (versionReporterStub) Report(context.Context) VersionReport {
	return VersionReport{Version: "1.0.0", FFmpegVersion: "9.0.2-full_build", Engine: "FDK-AAC 256 kb/s"}
}

func TestVersionController(t *testing.T) {
	var output bytes.Buffer

	code := NewVersionController(versionReporterStub{}, &output).Execute(context.Background(), nil)

	if code != 0 || output.String() != "music-ops 1.0.0\nffmpeg 9.0.2-full_build\nencoder FDK-AAC 256 kb/s\n" {
		t.Fatalf("code = %d output = %q", code, output.String())
	}
}
