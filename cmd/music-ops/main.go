package main

//go:generate go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.5.0 -64 -o resource_windows_amd64.syso -ver-major=$MUSIC_OPS_VERSION_MAJOR -ver-minor=$MUSIC_OPS_VERSION_MINOR -ver-patch=$MUSIC_OPS_VERSION_PATCH -product-ver-major=$MUSIC_OPS_VERSION_MAJOR -product-ver-minor=$MUSIC_OPS_VERSION_MINOR -product-ver-patch=$MUSIC_OPS_VERSION_PATCH -file-version=$MUSIC_OPS_VERSION -product-version=$MUSIC_OPS_VERSION ../../build/versioninfo.json

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/UrielJaloto/music-ops/internal/infrastructure/advisor"
	"github.com/UrielJaloto/music-ops/internal/infrastructure/artifacts"
	"github.com/UrielJaloto/music-ops/internal/infrastructure/config"
	"github.com/UrielJaloto/music-ops/internal/infrastructure/desktop"
	"github.com/UrielJaloto/music-ops/internal/infrastructure/faultinjection"
	"github.com/UrielJaloto/music-ops/internal/infrastructure/filesystem"
	"github.com/UrielJaloto/music-ops/internal/infrastructure/lastfm"
	"github.com/UrielJaloto/music-ops/internal/infrastructure/logging"
	"github.com/UrielJaloto/music-ops/internal/infrastructure/process"
	"github.com/UrielJaloto/music-ops/internal/infrastructure/runtime"
	"github.com/UrielJaloto/music-ops/internal/infrastructure/storage"
	"github.com/UrielJaloto/music-ops/internal/infrastructure/transcoding"
	"github.com/UrielJaloto/music-ops/internal/presentation/cli"
	"github.com/UrielJaloto/music-ops/internal/services/classification"
	"github.com/UrielJaloto/music-ops/internal/services/conversion"
	"github.com/UrielJaloto/music-ops/internal/services/crediting"
	"github.com/UrielJaloto/music-ops/internal/services/diagnostics"
	"github.com/UrielJaloto/music-ops/internal/services/discovery"
	"github.com/UrielJaloto/music-ops/internal/services/importing"
	"github.com/UrielJaloto/music-ops/internal/services/metadata"
	"github.com/UrielJaloto/music-ops/internal/services/publishing"
	"github.com/UrielJaloto/music-ops/internal/services/settings"
	"github.com/UrielJaloto/music-ops/internal/services/setup"
	"github.com/UrielJaloto/music-ops/internal/services/watching"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], terminal{input: os.Stdin, output: os.Stdout, errors: os.Stderr}, os.LookupEnv))
}

type terminal struct {
	input  io.Reader
	output io.Writer
	errors io.Writer
}

func run(arguments []string, console terminal, environment func(string) (string, bool)) int {
	errorPresenter := cli.NewErrorPresenter(console.errors)
	request, err := cli.ParseCommandLine(arguments)
	if err != nil {
		return errorPresenter.Present(err)
	}
	interrupted, stop := runtime.NewInterruptContext(context.Background(), os.Exit)
	defer stop()
	ctx, cancel := faultinjection.WithCancellation(interrupted, environment)
	defer cancel()
	paths := config.ReadEnvironmentPaths(environment)
	store := config.NewConfigStore(config.DefaultConfigPath(paths.AppData), environment)
	loaded, err := store.Load(ctx, request.Overrides)
	if err != nil {
		return errorPresenter.Present(err)
	}
	application := newWiring(wiringSettings{config: loaded, paths: paths, request: request, console: console, environment: environment, store: store})
	code := application.router().Route(ctx, request)
	application.close(context.WithoutCancel(ctx))
	desktop.NewConsoleSession(console.input, console.output).WaitForExit()
	return code
}

const (
	applicationFolder   = "music-ops"
	advisorFolder       = "advisor"
	stateFileName       = "state.db"
	logFileName         = "log.jsonl"
	shortHTTPTimeout    = 30 * time.Second
	advisorHTTPTimeout  = 10 * time.Minute
	healthHTTPTimeout   = 2 * time.Second
	advisorLoadTimeout  = 60 * time.Second
	bitsPerKilobit      = 1000
	versionEngineFormat = "%s %d kb/s"
)

var errWorkFolderUnknown = errors.New("LOCALAPPDATA is not set: cannot locate the work folder")

type wiringSettings struct {
	config      config.Config
	paths       config.EnvironmentPaths
	request     cli.RouterRequest
	console     terminal
	environment func(string) (string, bool)
	store       config.ConfigStore
}

type wiring struct {
	settings wiringSettings
	clock    runtime.SystemClock
	runner   process.ProcessRunner
	errors   cli.ErrorPresenter
	closers  []func(ctx context.Context) error
}

func newWiring(settings wiringSettings) *wiring {
	return &wiring{
		settings: settings,
		clock:    runtime.NewSystemClock(),
		runner:   process.NewProcessRunner(),
		errors:   cli.NewErrorPresenter(settings.console.errors),
	}
}

func (application *wiring) router() cli.CommandRouter {
	controllers := map[string]cli.CommandController{
		cli.CommandRun:     application.deferred(application.runCommand),
		cli.CommandPreview: application.deferred(application.previewCommand),
		cli.CommandWatch:   application.deferred(application.watchCommand),
		cli.CommandStatus:  application.statusCommand(),
		cli.CommandVersion: application.deferred(application.versionCommand),
		cli.CommandConfig:  application.configCommand(),
		cli.CommandAdvisor: application.advisorCommand(),
	}
	return cli.NewCommandRouter(controllers, application.settings.console.output)
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
	build  func(ctx context.Context) (cli.CommandController, error)
	errors cli.ErrorPresenter
}

func (application *wiring) deferred(build func(ctx context.Context) (cli.CommandController, error)) deferredCommand {
	return deferredCommand{build: build, errors: application.errors}
}

func (command deferredCommand) Execute(ctx context.Context, arguments []string) int {
	controller, err := command.build(ctx)
	if err != nil {
		return command.errors.Present(err)
	}
	return controller.Execute(ctx, arguments)
}

func (application *wiring) runCommand(ctx context.Context) (cli.CommandController, error) {
	controller, err := application.newImportController(ctx)
	if err != nil {
		return nil, err
	}
	return cli.NewRunCommand(controller), nil
}

func (application *wiring) previewCommand(ctx context.Context) (cli.CommandController, error) {
	controller, err := application.newImportController(ctx)
	if err != nil {
		return nil, err
	}
	return cli.NewPreviewCommand(controller), nil
}

func (application *wiring) configCommand() cli.CommandController {
	console := application.settings.console
	presenter := cli.NewSettingsPresenter(console.output, application.errors)
	return cli.NewSettingsController(settings.NewSettingsService(application.settings.store), presenter)
}

func (application *wiring) advisorCommand() cli.CommandController {
	presenter := cli.NewSetupPresenter(application.settings.console.output, application.errors)
	downloader := artifacts.NewHTTPArtifactDownloader(&http.Client{}, presenter)
	setup := setup.NewSetupService(downloader, artifacts.NewZipExtractor(), application.settings.store, presenter)
	artifacts := cli.AdvisorArtifacts{
		Folder:  filepath.Join(application.settings.paths.LocalAppData, applicationFolder, advisorFolder),
		Runtime: artifacts.BuildAdvisorRuntimeArtifacts(), Model: artifacts.BuildAdvisorModelArtifact(),
		ServerExecutableName: artifacts.AdvisorServerExecutable,
	}
	return cli.NewSetupController(setup, presenter, artifacts)
}

func (application *wiring) statusCommand() cli.CommandController {
	configured := application.settings.config
	ffmpeg := transcoding.FFmpegForDiagnostics(configured.Encoder.FFmpeg, exec.LookPath, application.settings.paths.LocalAppData)
	detector := transcoding.NewFFmpegCapabilityDetector(application.runner, ffmpeg)
	encoderProbe := transcoding.NewEncoderProbe(detector, transcoding.NewFDKAACEncoder(nil, configured.Encoder.FDKLibrary))
	diagnostics := diagnostics.NewDiagnosticsService(encoderProbe, application.newLastFMClient(), application.newAdvisorChain(), filesystem.NewOutputProbe(configured.Output.Dir))
	return cli.NewDiagnosticsController(diagnostics, cli.NewDiagnosticsPresenter(application.settings.console.output))
}

func (application *wiring) versionCommand(ctx context.Context) (cli.CommandController, error) {
	encoding, err := application.newEncoding(ctx)
	if err != nil {
		return nil, err
	}
	reporter := versionReporter{version: version, capabilities: encoding.capabilities, selector: encoding.selector}
	return cli.NewVersionController(reporter, application.settings.console.output), nil
}

type versionReporter struct {
	version      string
	capabilities transcoding.FFmpegCapabilities
	selector     transcoding.AACEncoderSelector
}

func (reporter versionReporter) Report(context.Context) cli.VersionReport {
	engine := fmt.Sprintf(versionEngineFormat, reporter.selector.EngineName(), reporter.selector.SelectProfile().Bitrate()/bitsPerKilobit)
	return cli.VersionReport{Version: reporter.version, FFmpegVersion: reporter.capabilities.Version, Engine: engine}
}

type encodingStack struct {
	ffmpeg       string
	ffprobe      string
	capabilities transcoding.FFmpegCapabilities
	selector     transcoding.AACEncoderSelector
}

func (application *wiring) newEncoding(ctx context.Context) (encodingStack, error) {
	config, localAppData := application.settings.config, application.settings.paths.LocalAppData
	ffmpeg, err := transcoding.LocateFFmpeg(config.Encoder.FFmpeg, exec.LookPath, localAppData)
	if err != nil {
		return encodingStack{}, err
	}
	ffprobe, err := transcoding.LocateFFprobe(config.Encoder.FFprobe, exec.LookPath, localAppData)
	if err != nil {
		return encodingStack{}, err
	}
	capabilities, err := transcoding.DetectFFmpegCapabilities(ctx, application.runner, ffmpeg)
	if err != nil {
		return encodingStack{}, err
	}
	decoder := transcoding.NewFFmpegDecoder(application.runner, ffmpeg, capabilities)
	selector := transcoding.NewAACEncoderSelector(
		transcoding.NewFDKAACEncoder(decoder, config.Encoder.FDKLibrary),
		transcoding.NewFFmpegAACEncoder(application.runner, ffmpeg, capabilities),
	)
	return encodingStack{ffmpeg: ffmpeg, ffprobe: ffprobe, capabilities: capabilities, selector: selector}, nil
}

type storageStack struct {
	staging filesystem.StagingArea
	store   *storage.BoltStore
	reports storage.ReportStorage
	logs    io.WriteCloser
}

func (application *wiring) newStorage(ctx context.Context) (storageStack, error) {
	if application.settings.paths.LocalAppData == "" {
		return storageStack{}, errWorkFolderUnknown
	}
	workRoot := filepath.Join(application.settings.paths.LocalAppData, applicationFolder)
	runID, idErr := runtime.NewRandomRunIDs().NewRunID(application.clock.Now())
	staging, err := filesystem.PrepareStagingArea(workRoot, runID)
	if err = errors.Join(idErr, err); err != nil {
		return storageStack{}, err
	}
	store, err := storage.OpenBoltStore(filepath.Join(workRoot, stateFileName))
	if err != nil {
		return storageStack{}, errors.Join(err, staging.Cleanup(ctx))
	}
	application.onClose(func(context.Context) error { return store.Close() })
	application.onClose(staging.Cleanup)
	if err := staging.RemoveOtherRuns(); err != nil {
		return storageStack{}, err
	}
	reports := storage.NewReportStorage(workRoot)
	logs, err := reports.Create(ctx, string(runID), logFileName)
	if err != nil {
		return storageStack{}, err
	}
	application.onClose(func(context.Context) error { return logs.Close() })
	return storageStack{staging: staging, store: store, reports: reports, logs: logs}, nil
}

func (application *wiring) newLastFMClient() lastfm.LastFMClient {
	configured := application.settings.config.LastFM
	settings := lastfm.LastFMSettings{APIKey: configured.APIKey, BaseURL: configured.BaseURL, Version: version}
	backoff := lastfm.NewExponentialBackoff(rand.Float64, application.clock)
	return lastfm.NewLastFMClient(settings, &http.Client{Timeout: shortHTTPTimeout}, lastfm.NewLastFMLimiter(), backoff)
}

func (application *wiring) newAdvisorChain() advisor.AdvisorChain {
	advisorConfig := application.settings.config.Advisor
	local := advisor.NewLlamaAdvisor(advisor.LlamaAdvisorSettings{
		BaseURL: advisor.BuildLocalAdvisorURL(advisorConfig.Port), Model: advisor.ExtractModelName(advisorConfig.ModelFile),
		LoadingTimeout: advisorLoadTimeout, Clock: application.clock,
	}, &http.Client{Timeout: advisorHTTPTimeout})
	server := advisor.NewLlamaServer(application.runner, advisor.LlamaServerSettings{
		Executable: advisorConfig.LlamaServer, ModelFile: advisorConfig.ModelFile, Port: advisorConfig.Port,
		HTTP: &http.Client{Timeout: healthHTTPTimeout}, Clock: application.clock,
	})
	lazy := advisor.NewLazyLlamaAdvisor(local, server)
	application.onClose(lazy.Close)
	chained := []advisor.ChainedAdvisor{lazy}
	return advisor.NewAdvisorChain(chained, advisor.IsValidCreditSuggestion, advisor.ValidGenreSuggestion)
}

var consoleLogWriters = map[bool]func(console terminal) io.Writer{
	true:  func(console terminal) io.Writer { return console.errors },
	false: func(terminal) io.Writer { return nil },
}

type importStack struct {
	importer logging.ImportServiceLogger
	reports  storage.ReportStorage
	defaults cli.ImportDefaults
}

func (application *wiring) newImportStack(ctx context.Context) (importStack, error) {
	encoding, err := application.newEncoding(ctx)
	if err != nil {
		return importStack{}, err
	}
	storage, err := application.newStorage(ctx)
	if err != nil {
		return importStack{}, err
	}
	logger := logging.NewLogger(storage.logs, application.consoleLog())
	progress := cli.NewProgressPresenter(application.settings.console.output, application.clock)
	pool := runtime.NewWorkerPool(application.settings.config.Encoder.Workers)
	importRuntime := runtime.NewImportRuntime(pool, application.clock, runtime.NewRandomRunIDs(), progress)
	importer := importing.NewImportService(
		application.newSourceService(encoding, pool),
		logging.NewConversionServiceLogger(application.newConversionService(encoding, storage, pool), logger, application.clock),
		application.newMetadataService(storage, pool),
		faultinjection.WrapPublisher(logging.NewPublishingServiceLogger(application.newPublishingService(encoding, storage), logger, application.clock), application.settings.environment),
		importRuntime,
	)
	return importStack{
		importer: logging.NewImportServiceLogger(importer, logger, application.clock),
		reports:  storage.reports,
		defaults: application.importDefaults(encoding),
	}, nil
}

func (application *wiring) consoleLog() io.Writer {
	return consoleLogWriters[application.settings.request.IsVerbose](application.settings.console)
}

func (application *wiring) importDefaults(encoding encodingStack) cli.ImportDefaults {
	configured := application.settings.config
	return cli.ImportDefaults{
		Output: configured.Output.Dir, Profile: encoding.selector.SelectProfile(), Numbering: configured.ParseNumbering(),
		Layout: configured.ParseLayout(), AlbumTemplate: configured.Output.AlbumTemplate,
	}
}

func (application *wiring) newSourceService(encoding encodingStack, pool runtime.WorkerPool) discovery.SourceService {
	inspector := transcoding.NewFFprobeInspector(application.runner, encoding.ffprobe)
	return discovery.NewSourceService(filesystem.NewFileScanner(), inspector, pool)
}

func (application *wiring) newConversionService(encoding encodingStack, stores storageStack, pool runtime.WorkerPool) conversion.ConversionService {
	copier := transcoding.NewFFmpegAudioCopier(application.runner, encoding.ffmpeg)
	journal := storage.NewJournalRepository(stores.store)
	return conversion.NewConversionService(encoding.selector, copier, stores.staging, journal, pool)
}

func (application *wiring) newMetadataService(stores storageStack, pool runtime.WorkerPool) metadata.MetadataService {
	cache := storage.NewBoltCache(stores.store, application.clock)
	chain := application.newAdvisorChain()
	batchSize := application.settings.config.Advisor.BatchSize
	credits := crediting.NewCreditService(advisor.NewCreditAdvisorCache(chain, cache, chain.Names()), batchSize)
	tags := lastfm.NewLastFMCache(application.newLastFMClient(), cache)
	genres := classification.NewGenreService(tags, advisor.NewGenreAdvisorCache(chain, cache, chain.Names()), pool, batchSize)
	return metadata.NewMetadataService(credits, genres)
}

func (application *wiring) newPublishingService(encoding encodingStack, stores storageStack) publishing.PublishingService {
	inspector := transcoding.NewFFprobeInspector(application.runner, encoding.ffprobe)
	return publishing.NewPublishingService(
		filesystem.NewLibraryIndex(),
		transcoding.NewFFmpegFinalizer(application.runner, encoding.ffmpeg),
		filesystem.NewFileMover(),
		transcoding.NewFFprobeVerifier(inspector),
		storage.NewJournalRepository(stores.store),
	)
}

func (application *wiring) newImportController(ctx context.Context) (cli.ImportController, error) {
	stack, err := application.newImportStack(ctx)
	if err != nil {
		return cli.ImportController{}, err
	}
	console := application.settings.console
	return cli.NewImportController(
		cli.ImportServices{Importer: stack.importer, Previewer: stack.importer, Reporter: cli.NewReportPresenter(stack.reports)},
		cli.ImportDesktop{Chooser: application.newFolderPicker(), Finisher: desktop.NewDesktopShell(application.runner, stack.reports)},
		cli.NewImportPresenter(console.output, application.errors),
		cli.ImportSettings{Defaults: stack.defaults, OpensFiles: application.settings.request.OpensFiles},
	), nil
}

func (application *wiring) newFolderPicker() desktop.FolderPicker {
	return desktop.NewFolderPicker(filepath.Join(application.settings.paths.AppData, applicationFolder))
}

func (application *wiring) watchCommand(ctx context.Context) (cli.CommandController, error) {
	stack, err := application.newImportStack(ctx)
	if err != nil {
		return nil, err
	}
	presenter := cli.NewWatchPresenter(application.settings.console.output, application.errors)
	watch := watching.NewWatchService(
		filesystem.NewFolderWatcher(application.clock), stack.importer,
		filesystem.NewFolderArchiver(), filesystem.NewFolderStability(application.clock), presenter,
	)
	configured := application.settings.config.Watch
	defaults := cli.WatchDefaults{Inbox: configured.Inbox, QuietPeriod: configured.Settle, Import: stack.defaults}
	return cli.NewWatchController(watch, presenter, defaults), nil
}
