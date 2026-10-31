package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/UrielJaloto/music-ops/infrastructure"
	"github.com/UrielJaloto/music-ops/presentation"
	"github.com/UrielJaloto/music-ops/services"
)

const (
	applicationFolder   = "music-ops"
	advisorFolder       = "advisor"
	workFolder          = ".music-ops"
	stateFileName       = "state.db"
	logFileName         = "log.jsonl"
	shortHTTPTimeout    = 30 * time.Second
	advisorHTTPTimeout  = 10 * time.Minute
	healthHTTPTimeout   = 2 * time.Second
	advisorLoadTimeout  = 60 * time.Second
	bitsPerKilobit      = 1000
	versionEngineFormat = "%s %d kb/s"
)

type wiringSettings struct {
	config      infrastructure.Config
	paths       infrastructure.EnvironmentPaths
	request     presentation.RouterRequest
	console     terminal
	environment func(string) (string, bool)
	store       infrastructure.ConfigStore
}

type wiring struct {
	settings wiringSettings
	clock    infrastructure.SystemClock
	runner   infrastructure.ProcessRunner
	errors   presentation.ErrorPresenter
	closers  []func(ctx context.Context) error
}

func newWiring(settings wiringSettings) *wiring {
	return &wiring{
		settings: settings,
		clock:    infrastructure.NewSystemClock(),
		runner:   infrastructure.NewProcessRunner(),
		errors:   presentation.NewErrorPresenter(settings.console.errors),
	}
}

func (application *wiring) router() presentation.CommandRouter {
	controllers := map[string]presentation.CommandController{
		presentation.CommandRun:     application.deferred(application.runCommand),
		presentation.CommandPreview: application.deferred(application.previewCommand),
		presentation.CommandWatch:   application.deferred(application.watchCommand),
		presentation.CommandDoctor:  application.doctorCommand(),
		presentation.CommandVersion: application.deferred(application.versionCommand),
		presentation.CommandConfig:  application.configCommand(),
		presentation.CommandAdvisor: application.advisorCommand(),
	}
	return presentation.NewCommandRouter(controllers, application.settings.console.output)
}

func (application *wiring) close(ctx context.Context) {
	for index := len(application.closers) - 1; index >= 0; index-- {
		if err := application.closers[index](ctx); err != nil {
			application.errors.Present(err)
		}
	}
}

func (application *wiring) onClose(closer func(ctx context.Context) error) {
	application.closers = append(application.closers, closer)
}

type deferredCommand struct {
	build  func(ctx context.Context) (presentation.CommandController, error)
	errors presentation.ErrorPresenter
}

func (application *wiring) deferred(build func(ctx context.Context) (presentation.CommandController, error)) deferredCommand {
	return deferredCommand{build: build, errors: application.errors}
}

func (command deferredCommand) Execute(ctx context.Context, arguments []string) int {
	controller, err := command.build(ctx)
	if err != nil {
		return command.errors.Present(err)
	}
	return controller.Execute(ctx, arguments)
}

func (application *wiring) runCommand(ctx context.Context) (presentation.CommandController, error) {
	controller, err := application.newImportController(ctx)
	if err != nil {
		return nil, err
	}
	return presentation.NewRunCommand(controller), nil
}

func (application *wiring) previewCommand(ctx context.Context) (presentation.CommandController, error) {
	controller, err := application.newImportController(ctx)
	if err != nil {
		return nil, err
	}
	return presentation.NewPreviewCommand(controller), nil
}

func (application *wiring) configCommand() presentation.CommandController {
	console := application.settings.console
	presenter := presentation.NewSettingsPresenter(console.output, application.errors)
	return presentation.NewSettingsController(services.NewSettingsService(application.settings.store), presenter)
}

func (application *wiring) advisorCommand() presentation.CommandController {
	presenter := presentation.NewSetupPresenter(application.settings.console.output, application.errors)
	downloader := infrastructure.NewHTTPArtifactDownloader(&http.Client{}, presenter)
	setup := services.NewSetupService(downloader, infrastructure.NewZipExtractor(), application.settings.store, presenter)
	artifacts := presentation.AdvisorArtifacts{
		Folder:  filepath.Join(application.settings.paths.LocalAppData, applicationFolder, advisorFolder),
		Runtime: infrastructure.AdvisorRuntimeArtifacts(), Model: infrastructure.AdvisorModelArtifact(),
		ServerExecutableName: infrastructure.AdvisorServerExecutable,
	}
	return presentation.NewSetupController(setup, presenter, artifacts)
}

func (application *wiring) doctorCommand() presentation.CommandController {
	config := application.settings.config
	ffmpeg := infrastructure.FFmpegForDiagnostics(config.Encoder.FFmpeg, exec.LookPath, application.settings.paths.LocalAppData)
	detector := infrastructure.NewFFmpegCapabilityDetector(application.runner, ffmpeg)
	encoderProbe := infrastructure.NewEncoderProbe(detector, infrastructure.NewFDKAACEncoder(nil, config.Encoder.FDKLibrary))
	diagnostics := services.NewDiagnosticsService(encoderProbe, application.newLastFMClient(), application.newAdvisorChain(), infrastructure.NewOutputProbe(config.Output.Dir))
	return presentation.NewDiagnosticsController(diagnostics, presentation.NewDiagnosticsPresenter(application.settings.console.output))
}

func (application *wiring) versionCommand(ctx context.Context) (presentation.CommandController, error) {
	encoding, err := application.newEncoding(ctx)
	if err != nil {
		return nil, err
	}
	reporter := versionReporter{version: version, capabilities: encoding.capabilities, selector: encoding.selector}
	return presentation.NewVersionController(reporter, application.settings.console.output), nil
}

type versionReporter struct {
	version      string
	capabilities infrastructure.FFmpegCapabilities
	selector     infrastructure.AACEncoderSelector
}

func (reporter versionReporter) Report(context.Context) presentation.VersionReport {
	engine := fmt.Sprintf(versionEngineFormat, reporter.selector.EngineName(), reporter.selector.Profile().Bitrate()/bitsPerKilobit)
	return presentation.VersionReport{Version: reporter.version, FFmpegVersion: reporter.capabilities.Version, Engine: engine}
}

type encodingStack struct {
	ffmpeg       string
	ffprobe      string
	capabilities infrastructure.FFmpegCapabilities
	selector     infrastructure.AACEncoderSelector
}

func (application *wiring) newEncoding(ctx context.Context) (encodingStack, error) {
	config, localAppData := application.settings.config, application.settings.paths.LocalAppData
	ffmpeg, err := infrastructure.LocateFFmpeg(config.Encoder.FFmpeg, exec.LookPath, localAppData)
	if err != nil {
		return encodingStack{}, err
	}
	ffprobe, err := infrastructure.LocateFFprobe(config.Encoder.FFprobe, exec.LookPath, localAppData)
	if err != nil {
		return encodingStack{}, err
	}
	capabilities, err := infrastructure.DetectFFmpegCapabilities(ctx, application.runner, ffmpeg)
	if err != nil {
		return encodingStack{}, err
	}
	decoder := infrastructure.NewFFmpegDecoder(application.runner, ffmpeg, capabilities)
	selector := infrastructure.NewAACEncoderSelector(
		infrastructure.NewFDKAACEncoder(decoder, config.Encoder.FDKLibrary),
		infrastructure.NewFFmpegAACEncoder(application.runner, ffmpeg, capabilities),
	)
	return encodingStack{ffmpeg: ffmpeg, ffprobe: ffprobe, capabilities: capabilities, selector: selector}, nil
}

type storageStack struct {
	staging infrastructure.StagingArea
	store   *infrastructure.BoltStore
	reports infrastructure.ReportStorage
	logs    io.WriteCloser
}

func (application *wiring) newStorage(ctx context.Context) (storageStack, error) {
	output := application.settings.config.Output.Dir
	runID, err := infrastructure.NewRandomRunIDs().NewRunID(application.clock.Now())
	if err != nil {
		return storageStack{}, err
	}
	staging, err := infrastructure.PrepareStagingArea(output, runID)
	if err != nil {
		return storageStack{}, err
	}
	store, err := infrastructure.OpenBoltStore(filepath.Join(output, workFolder, stateFileName))
	if err != nil {
		return storageStack{}, errors.Join(err, staging.Cleanup(ctx))
	}
	application.onClose(func(context.Context) error { return store.Close() })
	application.onClose(staging.Cleanup)
	if err := staging.RemoveOtherRuns(); err != nil {
		return storageStack{}, err
	}
	reports := infrastructure.NewReportStorage(output)
	logs, err := reports.Create(ctx, string(runID), logFileName)
	if err != nil {
		return storageStack{}, err
	}
	application.onClose(func(context.Context) error { return logs.Close() })
	return storageStack{staging: staging, store: store, reports: reports, logs: logs}, nil
}

func (application *wiring) newLastFMClient() infrastructure.LastFMClient {
	config := application.settings.config.LastFM
	settings := infrastructure.LastFMSettings{APIKey: config.APIKey, BaseURL: config.BaseURL, Version: version}
	backoff := infrastructure.NewExponentialBackoff(rand.Float64, application.clock)
	return infrastructure.NewLastFMClient(settings, &http.Client{Timeout: shortHTTPTimeout}, infrastructure.NewLastFMLimiter(), backoff)
}

func (application *wiring) newAdvisorChain() infrastructure.AdvisorChain {
	advisor := application.settings.config.Advisor
	local := infrastructure.NewLlamaAdvisor(infrastructure.LlamaAdvisorSettings{
		BaseURL: infrastructure.LocalAdvisorURL(advisor.Port), Model: infrastructure.LocalModelName(advisor.ModelFile),
		LoadingTimeout: advisorLoadTimeout, Clock: application.clock,
	}, &http.Client{Timeout: advisorHTTPTimeout})
	server := infrastructure.NewLlamaServer(application.runner, infrastructure.LlamaServerSettings{
		Executable: advisor.LlamaServer, ModelFile: advisor.ModelFile, Port: advisor.Port,
		HTTP: &http.Client{Timeout: healthHTTPTimeout}, Clock: application.clock,
	})
	lazy := infrastructure.NewLazyLlamaAdvisor(local, server)
	application.onClose(lazy.Close)
	chained := []infrastructure.ChainedAdvisor{lazy}
	return infrastructure.NewAdvisorChain(chained, infrastructure.ValidCreditSuggestion, infrastructure.ValidGenreSuggestion)
}
