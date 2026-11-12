package main

import (
	"context"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/UrielJaloto/music-ops/internal/infrastructure/advisor"
	"github.com/UrielJaloto/music-ops/internal/infrastructure/artifacts"
	"github.com/UrielJaloto/music-ops/internal/infrastructure/config"
	"github.com/UrielJaloto/music-ops/internal/infrastructure/desktop"
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
	"github.com/UrielJaloto/music-ops/internal/services/session"
	"github.com/UrielJaloto/music-ops/internal/services/settings"
	"github.com/UrielJaloto/music-ops/internal/services/setup"
	"github.com/UrielJaloto/music-ops/internal/services/watching"
)

var version = "dev"

const (
	advisorFolder = "advisor"
	stateFileName = "state.db"
)

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
	ctx, stop := runtime.NewInterruptContext(context.Background(), os.Exit)
	defer stop()
	locations := config.FindUserLocations()
	store := config.NewConfigStore(locations, environment)
	loaded, err := store.Load(ctx, request.Overrides)
	if err != nil {
		return errorPresenter.Present(err)
	}
	app := application{
		config: loaded, locations: locations, request: request, console: console, store: store,
		errors: errorPresenter, clock: runtime.NewSystemClock(), runner: process.NewProcessRunner(),
	}
	code := app.buildRouter().Route(ctx, request)
	desktop.NewConsoleSession(console.input, console.output).WaitForExit()
	return code
}

type application struct {
	config    config.Config
	locations config.Locations
	request   cli.RouterRequest
	console   terminal
	store     config.ConfigStore
	errors    cli.ErrorPresenter
	clock     runtime.SystemClock
	runner    process.ProcessRunner
}

type encoding struct {
	tools    transcoding.Toolchain
	fdk      transcoding.FDKAACEncoder
	selector transcoding.AACEncoderSelector
}

type stores struct {
	bolt    *storage.BoltStore
	staging *filesystem.StagingArea
	reports storage.ReportStorage
	runLog  *storage.RunLog
}

type parts struct {
	encoding encoding
	stores   stores
	chain    advisor.AdvisorChain
	tags     lastfm.LastFMClient
	pool     runtime.WorkerPool
	logger   *slog.Logger
}

func (app application) buildRouter() cli.CommandRouter {
	local := app.buildLocalAdvisor()
	graph := app.buildParts(local)
	checks := diagnostics.NewDiagnosticsService(version, transcoding.NewEncoderProbe(graph.encoding.tools, graph.encoding.fdk), graph.tags, graph.chain, filesystem.NewOutputProbe(app.config.Output.Dir))
	importRuntime := runtime.NewImportRuntime(graph.pool, app.clock, runtime.NewRandomRunIDs(), cli.NewProgressPresenter(app.console.output, app.clock))
	importer := app.buildImporter(graph, importRuntime)
	workspace := storage.NewWorkspace(app.locations.WorkDir, storage.WorkspaceParts{
		Store: graph.stores.bolt, Staging: graph.stores.staging, RunLog: graph.stores.runLog, Advisor: local, RunIDs: importRuntime,
	})
	watchPresenter := cli.NewWatchPresenter(app.console.output, app.errors)
	watch := watching.NewWatchService(filesystem.NewFolderWatcher(app.clock), importer, filesystem.NewFolderArchiver(), filesystem.NewFolderStability(app.clock), watchPresenter)
	runSession := session.NewSessionService(workspace, importer, watch, checks, desktop.NewFolderPicker(app.locations.ConfigDir))
	imports := app.buildImportController(runSession, graph)
	controllers := map[string]cli.CommandController{
		cli.CommandRun:     cli.NewRunCommand(imports),
		cli.CommandPreview: cli.NewPreviewCommand(imports),
		cli.CommandWatch:   cli.NewWatchController(runSession, watchPresenter, app.buildWatchDefaults(graph)),
		cli.CommandStatus:  cli.NewDiagnosticsController(runSession, cli.NewDiagnosticsPresenter(app.console.output, app.errors)),
		cli.CommandVersion: cli.NewVersionController(checks, app.console.output),
		cli.CommandConfig:  app.buildConfigCommand(),
		cli.CommandAdvisor: app.buildAdvisorCommand(),
	}
	return cli.NewCommandRouter(controllers, app.console.output)
}

func (app application) buildParts(local advisor.LazyLlamaAdvisor) parts {
	workDir := app.locations.WorkDir
	reports := storage.NewReportStorage(workDir)
	runLog := storage.NewRunLog(reports)
	return parts{
		encoding: app.buildEncoding(),
		stores:   stores{bolt: storage.NewBoltStore(filepath.Join(workDir, stateFileName)), staging: filesystem.NewStagingArea(workDir), reports: reports, runLog: runLog},
		chain:    advisor.NewAdvisorChain([]advisor.ChainedAdvisor{local}, advisor.IsValidCreditSuggestion, advisor.ValidGenreSuggestion),
		tags:     app.buildLastFMClient(),
		pool:     runtime.NewWorkerPool(app.config.Encoder.Workers),
		logger:   logging.NewLogger(runLog, cli.SelectLogOutput(app.request, app.console.errors)),
	}
}

func (app application) buildEncoding() encoding {
	configured := app.config.Encoder
	tools := transcoding.NewToolchain(transcoding.ToolchainSettings{FFmpeg: configured.FFmpeg, FFprobe: configured.FFprobe, LookPath: exec.LookPath}, app.runner)
	fdk := transcoding.NewFDKAACEncoder(transcoding.NewFFmpegDecoder(app.runner, tools), configured.FDKLibrary)
	selector := transcoding.NewAACEncoderSelector(fdk, transcoding.NewFFmpegAACEncoder(app.runner, tools))
	return encoding{tools: tools, fdk: fdk, selector: selector}
}

func (app application) buildLastFMClient() lastfm.LastFMClient {
	configured := app.config.LastFM
	settings := lastfm.LastFMSettings{APIKey: configured.APIKey, BaseURL: configured.BaseURL, Version: version}
	backoff := lastfm.NewExponentialBackoff(rand.Float64, app.clock)
	return lastfm.NewLastFMClient(settings, &http.Client{Timeout: lastfm.HTTPTimeout}, lastfm.NewLastFMLimiter(), backoff)
}

func (app application) buildLocalAdvisor() advisor.LazyLlamaAdvisor {
	configured := app.config.Advisor
	local := advisor.NewLlamaAdvisor(advisor.LlamaAdvisorSettings{
		BaseURL: advisor.BuildLocalAdvisorURL(configured.Port), Model: advisor.ExtractModelName(configured.ModelFile),
		LoadingTimeout: advisor.ModelLoadingTimeout, Clock: app.clock,
	}, &http.Client{Timeout: advisor.CompletionTimeout})
	server := advisor.NewLlamaServer(app.runner, advisor.LlamaServerSettings{
		Executable: configured.LlamaServer, ModelFile: configured.ModelFile, Port: configured.Port,
		HTTP: &http.Client{Timeout: advisor.HealthTimeout}, Clock: app.clock,
	})
	return advisor.NewLazyLlamaAdvisor(local, server)
}

func (app application) buildImporter(graph parts, importRuntime runtime.ImportRuntime) logging.ImportServiceLogger {
	inspector := transcoding.NewFFprobeInspector(app.runner, graph.encoding.tools)
	journal := storage.NewJournalRepository(graph.stores.bolt)
	converter := conversion.NewConversionService(graph.encoding.selector, transcoding.NewFFmpegAudioCopier(app.runner, graph.encoding.tools), graph.stores.staging, journal, graph.pool)
	publisher := publishing.NewPublishingService(
		filesystem.NewLibraryIndex(), transcoding.NewFFmpegFinalizer(app.runner, graph.encoding.tools),
		filesystem.NewFileMover(), transcoding.NewFFprobeVerifier(inspector), journal,
	)
	importer := importing.NewImportService(
		discovery.NewSourceService(filesystem.NewFileScanner(), inspector, graph.pool),
		logging.NewConversionServiceLogger(converter, graph.logger, app.clock),
		app.buildMetadata(graph),
		logging.NewPublishingServiceLogger(publisher, graph.logger, app.clock),
		importRuntime,
	)
	return logging.NewImportServiceLogger(importer, graph.logger, app.clock)
}

func (app application) buildMetadata(graph parts) metadata.MetadataService {
	advice := storage.NewBoltCache(graph.stores.bolt, app.clock, advisor.CacheBucket)
	batchSize := app.config.Advisor.BatchSize
	credits := crediting.NewCreditService(advisor.NewCreditAdvisorCache(graph.chain, advice, graph.chain.Names()), batchSize)
	tags := lastfm.NewLastFMCache(graph.tags, storage.NewBoltCache(graph.stores.bolt, app.clock, lastfm.CacheBucket))
	genres := classification.NewGenreService(tags, advisor.NewGenreAdvisorCache(graph.chain, advice, graph.chain.Names()), graph.pool, batchSize)
	return metadata.NewMetadataService(credits, genres)
}

func (app application) buildImportDefaults(graph parts) cli.ImportDefaults {
	return cli.ImportDefaults{
		Output: app.config.Output.Dir, Profile: graph.encoding.selector, Numbering: app.config.ParseNumbering(),
		Layout: app.config.ParseLayout(), AlbumTemplate: app.config.Output.AlbumTemplate, Tags: app.request.Tags,
	}
}

func (app application) buildImportController(runSession session.SessionService, graph parts) cli.ImportController {
	return cli.NewImportController(
		cli.ImportServices{Importer: runSession, Previewer: runSession, Reporter: cli.NewReportPresenter(graph.stores.reports)},
		desktop.NewDesktopShell(graph.stores.reports),
		cli.NewImportPresenter(app.console.output, app.errors),
		cli.ImportSettings{Defaults: app.buildImportDefaults(graph), OpensFiles: app.request.OpensFiles},
	)
}

func (app application) buildWatchDefaults(graph parts) cli.WatchDefaults {
	configured := app.config.Watch
	return cli.WatchDefaults{Inbox: configured.Inbox, QuietPeriod: configured.Settle, Import: app.buildImportDefaults(graph)}
}

func (app application) buildConfigCommand() cli.CommandController {
	presenter := cli.NewSettingsPresenter(app.console.output, app.errors)
	return cli.NewSettingsController(settings.NewSettingsService(app.store), presenter)
}

func (app application) buildAdvisorCommand() cli.CommandController {
	presenter := cli.NewSetupPresenter(app.console.output, app.errors)
	downloader := artifacts.NewHTTPArtifactDownloader(&http.Client{}, presenter)
	installer := setup.NewSetupService(downloader, artifacts.NewZipExtractor(), app.store, presenter)
	advisorArtifacts := cli.AdvisorArtifacts{
		Folder:  filepath.Join(app.locations.WorkDir, advisorFolder),
		Runtime: artifacts.BuildAdvisorRuntimeArtifacts(), Model: artifacts.BuildAdvisorModelArtifact(),
		ServerExecutableName: artifacts.AdvisorServerExecutable,
	}
	return cli.NewSetupController(installer, presenter, advisorArtifacts)
}
