package services

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type setupRecorder struct {
	events []string
}

type downloaderSpy struct {
	recorder *setupRecorder
	failOn   string
}

func (spy *downloaderSpy) Download(_ context.Context, artifact ArtifactRequest, destination string) error {
	spy.recorder.events = append(spy.recorder.events, "download "+artifact.Name+" -> "+destination)
	if artifact.Name == spy.failOn {
		return errTechnical
	}
	return nil
}

type extractorSpy struct {
	recorder *setupRecorder
	err      error
}

func (spy *extractorSpy) Extract(_ context.Context, archive, destination string) error {
	spy.recorder.events = append(spy.recorder.events, "extract "+archive+" -> "+destination)
	return spy.err
}

type setupSettingsSpy struct {
	recorder *setupRecorder
	failKey  string
}

func (spy *setupSettingsSpy) Save(_ context.Context, key, value string) error {
	spy.recorder.events = append(spy.recorder.events, "save "+key+"="+value)
	if key == spy.failKey {
		return errTechnical
	}
	return nil
}

type setupProgressSpy struct {
	recorder *setupRecorder
}

func (spy setupProgressSpy) ArtifactStarted(name string, _ int64) {
	spy.recorder.events = append(spy.recorder.events, "started "+name)
}

func (spy setupProgressSpy) ArtifactProgressed(string, int64) {}

func (spy setupProgressSpy) ArtifactFinished(name string) {
	spy.recorder.events = append(spy.recorder.events, "finished "+name)
}

type setupDoubles struct {
	recorder   *setupRecorder
	downloader *downloaderSpy
	extractor  *extractorSpy
	settings   *setupSettingsSpy
}

func newSetupDoubles() setupDoubles {
	recorder := &setupRecorder{}
	return setupDoubles{recorder: recorder, downloader: &downloaderSpy{recorder: recorder}, extractor: &extractorSpy{recorder: recorder}, settings: &setupSettingsSpy{recorder: recorder}}
}

func (doubles setupDoubles) service() SetupService {
	return NewSetupService(doubles.downloader, doubles.extractor, doubles.settings, setupProgressSpy{recorder: doubles.recorder})
}

func setupInput() InstallAdvisorInput {
	return InstallAdvisorInput{
		Folder:  "C:/advisor/",
		Runtime: []ArtifactRequest{{Name: "llama.zip", URL: "https://example/llama.zip"}, {Name: "cudart.zip", URL: "https://example/cudart.zip"}},
		Model:   ArtifactRequest{Name: "model.gguf", URL: "https://example/model.gguf"},
	}
}

func TestSetupService_InstallAdvisor_HappyPath(t *testing.T) {
	doubles := newSetupDoubles()

	installed, err := doubles.service().InstallAdvisor(context.Background(), setupInput())

	expected := []string{
		"started llama.zip", "download llama.zip -> C:/advisor/downloads/llama.zip", "finished llama.zip",
		"extract C:/advisor/downloads/llama.zip -> C:/advisor/llama.cpp",
		"started cudart.zip", "download cudart.zip -> C:/advisor/downloads/cudart.zip", "finished cudart.zip",
		"extract C:/advisor/downloads/cudart.zip -> C:/advisor/llama.cpp",
		"started model.gguf", "download model.gguf -> C:/advisor/models/model.gguf", "finished model.gguf",
		"save advisor.llama_server=C:/advisor/llama.cpp/llama-server.exe",
		"save advisor.model_file=C:/advisor/models/model.gguf",
	}
	if err != nil || !slices.Equal(doubles.recorder.events, expected) {
		t.Fatalf("events = %v err = %v", doubles.recorder.events, err)
	}
	if installed != (InstalledAdvisorOutput{ServerPath: "C:/advisor/llama.cpp/llama-server.exe", ModelPath: "C:/advisor/models/model.gguf"}) {
		t.Fatalf("installed = %+v", installed)
	}
}

func TestSetupService_InstallAdvisor_CustomExecutable(t *testing.T) {
	input := setupInput()
	input.ServerExecutableName = "llama-server-cuda.exe"

	installed, err := newSetupDoubles().service().InstallAdvisor(context.Background(), input)

	if err != nil || installed.ServerPath != "C:/advisor/llama.cpp/llama-server-cuda.exe" {
		t.Fatalf("installed = %+v err = %v", installed, err)
	}
}

func TestSetupService_InstallAdvisor_Failures(t *testing.T) {
	cases := []struct {
		name          string
		arrange       func(setupDoubles)
		lastEvent     string
		expectedError error
	}{
		{name: "RuntimeDownloadFails_StopsBeforeSaving", arrange: func(d setupDoubles) { d.downloader.failOn = "llama.zip" }, lastEvent: "download llama.zip -> C:/advisor/downloads/llama.zip", expectedError: errTechnical},
		{name: "ExtractFails_StopsBeforeModelDownload", arrange: func(d setupDoubles) { d.extractor.err = errTechnical }, lastEvent: "extract C:/advisor/downloads/llama.zip -> C:/advisor/llama.cpp", expectedError: errTechnical},
		{name: "ModelDownloadFails_StopsBeforeSaving", arrange: func(d setupDoubles) { d.downloader.failOn = "model.gguf" }, lastEvent: "download model.gguf -> C:/advisor/models/model.gguf", expectedError: errTechnical},
		{name: "SaveServerFails_StopsBeforeModelSetting", arrange: func(d setupDoubles) { d.settings.failKey = SettingLlamaServer }, lastEvent: "save advisor.llama_server=C:/advisor/llama.cpp/llama-server.exe", expectedError: errTechnical},
		{name: "SaveModelFails_ReturnsError", arrange: func(d setupDoubles) { d.settings.failKey = SettingModelFile }, lastEvent: "save advisor.model_file=C:/advisor/models/model.gguf", expectedError: errTechnical},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			doubles := newSetupDoubles()
			testCase.arrange(doubles)

			_, err := doubles.service().InstallAdvisor(context.Background(), setupInput())

			events := doubles.recorder.events
			if !errors.Is(err, testCase.expectedError) || events[len(events)-1] != testCase.lastEvent {
				t.Fatalf("err = %v events = %v", err, events)
			}
		})
	}
}

func TestSetupService_InstallAdvisor_Validation(t *testing.T) {
	cases := []struct {
		name          string
		change        func(*InstallAdvisorInput)
		expectedError error
	}{
		{name: "EmptyFolder_ReturnsErrFolderMissing", change: func(input *InstallAdvisorInput) { input.Folder = " " }, expectedError: ErrFolderMissing},
		{name: "NoRuntime_ReturnsErrSetupArtifactMissing", change: func(input *InstallAdvisorInput) { input.Runtime = nil }, expectedError: ErrSetupArtifactMissing},
		{name: "EmptyModelURL_ReturnsErrSetupArtifactMissing", change: func(input *InstallAdvisorInput) { input.Model.URL = "" }, expectedError: ErrSetupArtifactMissing},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			doubles := newSetupDoubles()
			input := setupInput()
			testCase.change(&input)

			_, err := doubles.service().InstallAdvisor(context.Background(), input)

			if !errors.Is(err, testCase.expectedError) || len(doubles.recorder.events) != 0 {
				t.Fatalf("err = %v events = %v", err, doubles.recorder.events)
			}
		})
	}
}
