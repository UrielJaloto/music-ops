package presentation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/UrielJaloto/music-ops/services"
)

type controllerSpy struct {
	code      int
	calls     int
	arguments []string
}

func (spy *controllerSpy) Execute(_ context.Context, arguments []string) int {
	spy.calls++
	spy.arguments = arguments
	return spy.code
}

func readGolden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(filepath.Join("testdata", name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func newRouterWithSpies() (CommandRouter, map[string]*controllerSpy, *bytes.Buffer) {
	spies := map[string]*controllerSpy{}
	controllers := map[string]CommandController{}
	for index, command := range []string{CommandRun, CommandPreview, CommandConfig, CommandDoctor, CommandWatch, CommandAdvisor, CommandVersion} {
		spy := &controllerSpy{code: 10 + index}
		spies[command] = spy
		controllers[command] = spy
	}
	output := &bytes.Buffer{}
	return NewCommandRouter(controllers, output), spies, output
}

func TestCommandRouter_RoutesEveryCommand(t *testing.T) {
	cases := []struct {
		arguments         []string
		expectedCommand   string
		expectedArguments []string
	}{
		{arguments: []string{}, expectedCommand: CommandRun, expectedArguments: []string{}},
		{arguments: []string{"run", "D:/In"}, expectedCommand: CommandRun, expectedArguments: []string{"D:/In"}},
		{arguments: []string{"preview", "D:/In"}, expectedCommand: CommandPreview, expectedArguments: []string{"D:/In"}},
		{arguments: []string{"config", "set", "output.dir", "E:/Music"}, expectedCommand: CommandConfig, expectedArguments: []string{"set", "output.dir", "E:/Music"}},
		{arguments: []string{"doctor"}, expectedCommand: CommandDoctor, expectedArguments: []string{}},
		{arguments: []string{"watch", "D:/Inbox"}, expectedCommand: CommandWatch, expectedArguments: []string{"D:/Inbox"}},
		{arguments: []string{"advisor", "install"}, expectedCommand: CommandAdvisor, expectedArguments: []string{"install"}},
		{arguments: []string{"version"}, expectedCommand: CommandVersion, expectedArguments: []string{}},
	}
	for _, testCase := range cases {
		t.Run(fmt.Sprint(testCase.arguments), func(t *testing.T) {
			router, spies, output := newRouterWithSpies()
			request, err := ParseCommandLine(testCase.arguments)
			if err != nil {
				t.Fatal(err)
			}

			code := router.Route(context.Background(), request)

			spy := spies[testCase.expectedCommand]
			if code != spy.code || spy.calls != 1 || output.Len() != 0 {
				t.Fatalf("code = %d calls = %d output = %q", code, spy.calls, output.String())
			}
			if diff := cmp.Diff(testCase.expectedArguments, spy.arguments); diff != "" {
				t.Fatalf("arguments (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCommandRouter_HelpAndUnknown(t *testing.T) {
	cases := []struct {
		command      string
		expectedCode int
	}{
		{command: "help", expectedCode: 0},
		{command: "-h", expectedCode: 0},
		{command: "--help", expectedCode: 0},
		{command: "explode", expectedCode: 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.command, func(t *testing.T) {
			router, spies, output := newRouterWithSpies()

			code := router.Route(context.Background(), RouterRequest{Command: testCase.command})

			if code != testCase.expectedCode || output.String() != readGolden(t, "usage.golden") || spies[CommandRun].calls != 0 {
				t.Fatalf("code = %d output = %q", code, output.String())
			}
		})
	}
}

func TestParseCommandLine_Flags(t *testing.T) {
	cases := []struct {
		name      string
		arguments []string
		expected  RouterRequest
	}{
		{
			name:      "OutAndLayout",
			arguments: []string{"run", "D:/In", "--out", "E:/Music", "--layout", "album_folders"},
			expected:  RouterRequest{Command: "run", Arguments: []string{"D:/In"}, Overrides: map[string]string{"output.dir": "E:/Music", "output.layout": "album_folders"}, OpensFiles: true},
		},
		{
			name:      "RepeatedSet",
			arguments: []string{"preview", "--set", "advisor.port=9000", "--set=lastfm.api_key=abc=", "D:/In"},
			expected:  RouterRequest{Command: "preview", Arguments: []string{"D:/In"}, Overrides: map[string]string{"advisor.port": "9000", "lastfm.api_key": "abc="}, OpensFiles: true},
		},
		{
			name:      "OutBeatsSetForSameKey",
			arguments: []string{"run", "--set", "output.dir=A", "--out", "B"},
			expected:  RouterRequest{Command: "run", Arguments: []string{}, Overrides: map[string]string{"output.dir": "B"}, OpensFiles: true},
		},
		{
			name:      "VerboseAndNoOpen",
			arguments: []string{"watch", "--verbose", "--no-open"},
			expected:  RouterRequest{Command: "watch", Arguments: []string{}, Overrides: map[string]string{}, IsVerbose: true},
		},
		{
			name:      "ConfigKeepsRawArguments",
			arguments: []string{"config", "set", "output.dir", "--weird"},
			expected:  RouterRequest{Command: "config", Arguments: []string{"set", "output.dir", "--weird"}, Overrides: map[string]string{}, OpensFiles: true},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request, err := ParseCommandLine(testCase.arguments)

			if diff := cmp.Diff(testCase.expected, request); diff != "" || err != nil {
				t.Fatalf("request (-want +got):\n%s err = %v", diff, err)
			}
		})
	}
}

func TestParseCommandLine_InvalidFlags(t *testing.T) {
	cases := [][]string{
		{"run", "--set", "no-divider"},
		{"run", "--set", "=value"},
		{"run", "--color"},
	}
	for _, arguments := range cases {
		t.Run(fmt.Sprint(arguments), func(t *testing.T) {
			_, err := ParseCommandLine(arguments)

			if !errors.Is(err, ErrCommandLineInvalid) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestErrorPresenter_Present(t *testing.T) {
	cases := []struct {
		err             error
		expectedMessage string
		expectedCode    int
	}{
		{err: context.Canceled, expectedMessage: "cancelled", expectedCode: 130},
		{err: services.ErrFolderMissing, expectedMessage: "the folder is missing or does not exist", expectedCode: 1},
		{err: services.ErrOutputInsideInput, expectedMessage: "the output folder must not be inside the input folder", expectedCode: 1},
		{err: services.ErrSettingUnknown, expectedMessage: "unknown setting; run: music-ops config show", expectedCode: 1},
		{err: services.ErrSettingInvalid, expectedMessage: "invalid setting value", expectedCode: 1},
		{err: services.ErrSettingKeyMissing, expectedMessage: "a setting key is required", expectedCode: 1},
		{err: services.ErrLibraryBusy, expectedMessage: "another music-ops run is using this library", expectedCode: 1},
		{err: services.ErrFFmpegMissing, expectedMessage: "ffmpeg was not found; install it with: winget install -e --id Gyan.FFmpeg", expectedCode: 1},
		{err: services.ErrLocalAdvisorNotInstalled, expectedMessage: "the local advisor is not installed; run: music-ops advisor install", expectedCode: 1},
		{err: services.ErrLastFMKeyInvalid, expectedMessage: "the Last.fm API key is invalid; run: music-ops config set lastfm.api_key <key>", expectedCode: 1},
		{err: ErrCommandLineInvalid, expectedMessage: "invalid command line; run: music-ops help", expectedCode: 1},
		{err: fmt.Errorf("opening library: %w", services.ErrLibraryBusy), expectedMessage: "another music-ops run is using this library", expectedCode: 1},
		{err: errors.New("disk on fire"), expectedMessage: "music-ops failed: disk on fire", expectedCode: 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.expectedMessage, func(t *testing.T) {
			var output bytes.Buffer

			code := NewErrorPresenter(&output).Present(testCase.err)

			if code != testCase.expectedCode || output.String() != "error: "+testCase.expectedMessage+"\n" {
				t.Fatalf("code = %d output = %q", code, output.String())
			}
		})
	}
}
