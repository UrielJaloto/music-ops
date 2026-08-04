package presentation

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
)

type inboxWatcherSpy struct {
	inputs []services.WatchInput
	err    error
}

func (spy *inboxWatcherSpy) Watch(_ context.Context, input services.WatchInput) error {
	spy.inputs = append(spy.inputs, input)
	return spy.err
}

func runWatch(watcher *inboxWatcherSpy, defaultInbox string, arguments ...string) (int, string) {
	var output bytes.Buffer
	defaults := WatchDefaults{Inbox: defaultInbox, QuietPeriod: 20 * time.Second, Import: testDefaults()}
	code := NewWatchController(watcher, NewWatchPresenter(&output, NewErrorPresenter(&output)), defaults).Execute(context.Background(), arguments)
	return code, output.String()
}

func TestWatchController(t *testing.T) {
	cases := []struct {
		name          string
		defaultInbox  string
		arguments     []string
		expectedInbox string
	}{
		{name: "ArgumentInbox", defaultInbox: "D:/Configured", arguments: []string{"D:/Argument"}, expectedInbox: "D:/Argument"},
		{name: "ConfiguredInbox", defaultInbox: "D:/Configured", expectedInbox: "D:/Configured"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			watcher := &inboxWatcherSpy{}

			code, output := runWatch(watcher, testCase.defaultInbox, testCase.arguments...)

			input := watcher.inputs[0]
			if code != 0 || output != "watch stopped\n" || input.Inbox != testCase.expectedInbox || input.QuietPeriod != 20*time.Second || input.Import.Output != "E:/Music" || input.Import.Layout != domain.LayoutFlat {
				t.Fatalf("code = %d output = %q input = %+v", code, output, input)
			}
		})
	}
}

func TestWatchController_MissingInbox(t *testing.T) {
	watcher := &inboxWatcherSpy{}

	code, output := runWatch(watcher, "")

	if code != 1 || output != "usage: music-ops watch [inbox] (or set watch.inbox)\n" || len(watcher.inputs) != 0 {
		t.Fatalf("code = %d output = %q", code, output)
	}
}

func TestWatchController_WatchError(t *testing.T) {
	code, output := runWatch(&inboxWatcherSpy{err: services.ErrFolderMissing}, "D:/Inbox")

	if code != 1 || output != "error: the folder is missing or does not exist\n" {
		t.Fatalf("code = %d output = %q", code, output)
	}
}

func TestWatchPresenter_Progress(t *testing.T) {
	var output bytes.Buffer
	presenter := NewWatchPresenter(&output, NewErrorPresenter(&output))

	presenter.FolderImported("D:/Inbox/Album", 0)
	presenter.FolderImported("D:/Inbox/Other", 2)
	presenter.FolderFailed("D:/Inbox/Broken", "folder is missing")

	expected := "imported D:/Inbox/Album (exit 0)\nimported D:/Inbox/Other (exit 2)\nfailed D:/Inbox/Broken: folder is missing\n"
	if output.String() != expected {
		t.Fatalf("output = %q", output.String())
	}
}

type advisorInstallerSpy struct {
	inputs []services.InstallAdvisorInput
	err    error
}

func (spy *advisorInstallerSpy) InstallAdvisor(_ context.Context, input services.InstallAdvisorInput) (services.InstalledAdvisorOutput, error) {
	spy.inputs = append(spy.inputs, input)
	return services.InstalledAdvisorOutput{ServerPath: "C:/advisor/llama.cpp/llama-server.exe", ModelPath: "C:/advisor/models/model.gguf"}, spy.err
}

func testArtifacts() AdvisorArtifacts {
	return AdvisorArtifacts{
		Folder:  "C:/advisor",
		Runtime: []services.ArtifactRequest{{Name: "llama.zip", URL: "https://example.invalid/llama.zip", SHA256: "aa", SizeBytes: 10}},
		Model:   services.ArtifactRequest{Name: "model.gguf", URL: "https://example.invalid/model.gguf", SHA256: "bb", SizeBytes: 20}, ServerExecutableName: "llama-server.exe",
	}
}

func TestSetupController(t *testing.T) {
	cases := []struct {
		name         string
		arguments    []string
		err          error
		expectedCode int
		expectedText string
		expectedRuns int
	}{
		{name: "Install", arguments: []string{"install"}, expectedCode: 0, expectedText: "advisor installed\n  server: C:/advisor/llama.cpp/llama-server.exe\n  model:  C:/advisor/models/model.gguf\n", expectedRuns: 1},
		{name: "MissingSubcommand", arguments: []string{}, expectedCode: 1, expectedText: "usage: music-ops advisor install\n"},
		{name: "UnknownSubcommand", arguments: []string{"remove"}, expectedCode: 1, expectedText: "usage: music-ops advisor install\n"},
		{name: "InstallFails", arguments: []string{"install"}, err: services.ErrSetupArtifactMissing, expectedCode: 1, expectedText: "error: music-ops failed: advisor runtime or model artifact is missing\n", expectedRuns: 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var output bytes.Buffer
			installer := &advisorInstallerSpy{err: testCase.err}

			code := NewSetupController(installer, NewSetupPresenter(&output, NewErrorPresenter(&output)), testArtifacts()).Execute(context.Background(), testCase.arguments)

			if code != testCase.expectedCode || output.String() != testCase.expectedText || len(installer.inputs) != testCase.expectedRuns {
				t.Fatalf("code = %d output = %q runs = %d", code, output.String(), len(installer.inputs))
			}
			if testCase.expectedRuns == 1 && (installer.inputs[0].Folder != "C:/advisor" || installer.inputs[0].Model.Name != "model.gguf" || len(installer.inputs[0].Runtime) != 1) {
				t.Fatalf("input = %+v", installer.inputs[0])
			}
		})
	}
}

func TestSetupPresenter_ProgressGolden(t *testing.T) {
	var output bytes.Buffer
	presenter := NewSetupPresenter(&output, NewErrorPresenter(&output))

	presenter.ArtifactStarted("model.gguf", 50<<20)
	for done := int64(0); done <= 50<<20; done += 4 << 20 {
		presenter.ArtifactProgressed("model.gguf", done)
	}
	presenter.ArtifactProgressed("model.gguf", 50<<20)
	presenter.ArtifactProgressed("unknown.zip", 10)
	presenter.ArtifactFinished("model.gguf")

	assertGolden(t, "setup_progress.golden", output.String())
}
