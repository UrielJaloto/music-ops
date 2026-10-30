package main

import (
	"context"
	"io"
	"path/filepath"

	"github.com/UrielJaloto/music-ops/infrastructure"
	"github.com/UrielJaloto/music-ops/presentation"
	"github.com/UrielJaloto/music-ops/services"
)

var consoleLogWriters = map[bool]func(console terminal) io.Writer{
	true:  func(console terminal) io.Writer { return console.errors },
	false: func(terminal) io.Writer { return nil },
}

type importStack struct {
	importer infrastructure.ImportServiceLogger
	reports  infrastructure.ReportStorage
	defaults presentation.ImportDefaults
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
	logger := infrastructure.NewLogger(storage.logs, application.consoleLog())
	progress := presentation.NewProgressPresenter(application.settings.console.output, application.clock)
	pool := infrastructure.NewWorkerPool(application.settings.config.Encoder.Workers)
	runtime := infrastructure.NewImportRuntime(pool, application.clock, infrastructure.NewRandomRunIDs(), progress)
	importer := services.NewImportService(
		application.newSourceService(encoding, pool),
		infrastructure.NewConversionServiceLogger(application.newConversionService(encoding, storage, pool), logger, application.clock),
		application.newMetadataService(storage, pool),
		withTestHooks(infrastructure.NewPublishingServiceLogger(application.newPublishingService(encoding, storage), logger, application.clock), application.settings.environment),
		runtime,
	)
	return importStack{
		importer: infrastructure.NewImportServiceLogger(importer, logger, application.clock),
		reports:  storage.reports,
		defaults: application.importDefaults(encoding),
	}, nil
}

func (application *wiring) consoleLog() io.Writer {
	return consoleLogWriters[application.settings.request.IsVerbose](application.settings.console)
}

func (application *wiring) importDefaults(encoding encodingStack) presentation.ImportDefaults {
	config := application.settings.config
	return presentation.ImportDefaults{
		Output: config.Output.Dir, Profile: encoding.selector.Profile(), Numbering: config.Numbering(),
		Layout: config.Layout(), AlbumTemplate: config.Output.AlbumTemplate,
	}
}

func (application *wiring) newSourceService(encoding encodingStack, pool infrastructure.WorkerPool) services.SourceService {
	inspector := infrastructure.NewFFprobeInspector(application.runner, encoding.ffprobe)
	return services.NewSourceService(infrastructure.NewFileScanner(), inspector, pool)
}

func (application *wiring) newConversionService(encoding encodingStack, storage storageStack, pool infrastructure.WorkerPool) services.ConversionService {
	copier := infrastructure.NewFFmpegAudioCopier(application.runner, encoding.ffmpeg)
	journal := infrastructure.NewJournalRepository(storage.store)
	return services.NewConversionService(encoding.selector, copier, storage.staging, journal, pool)
}

func (application *wiring) newMetadataService(storage storageStack, pool infrastructure.WorkerPool) services.MetadataService {
	cache := infrastructure.NewBoltCache(storage.store, application.clock)
	chain := application.newAdvisorChain()
	batchSize := application.settings.config.Advisor.BatchSize
	credits := services.NewCreditService(infrastructure.NewCreditAdvisorCache(chain, cache, chain.Names()), batchSize)
	tags := infrastructure.NewLastFMCache(application.newLastFMClient(), cache)
	genres := services.NewGenreService(tags, infrastructure.NewGenreAdvisorCache(chain, cache, chain.Names()), pool, batchSize)
	return services.NewMetadataService(credits, genres)
}

func (application *wiring) newPublishingService(encoding encodingStack, storage storageStack) services.PublishingService {
	inspector := infrastructure.NewFFprobeInspector(application.runner, encoding.ffprobe)
	return services.NewPublishingService(
		infrastructure.NewLibraryIndex(),
		infrastructure.NewFFmpegFinalizer(application.runner, encoding.ffmpeg),
		infrastructure.NewFileMover(),
		infrastructure.NewFFprobeVerifier(inspector),
		infrastructure.NewJournalRepository(storage.store),
	)
}

func (application *wiring) newImportController(ctx context.Context) (presentation.ImportController, error) {
	stack, err := application.newImportStack(ctx)
	if err != nil {
		return presentation.ImportController{}, err
	}
	console := application.settings.console
	return presentation.NewImportController(
		presentation.ImportServices{Importer: stack.importer, Previewer: stack.importer, Reporter: presentation.NewReportPresenter(stack.reports)},
		presentation.ImportDesktop{Chooser: application.newFolderPicker(), Finisher: infrastructure.NewDesktopShell(application.runner, stack.reports)},
		presentation.NewImportPresenter(console.output, application.errors),
		presentation.ImportSettings{Defaults: stack.defaults, OpensFiles: application.settings.request.OpensFiles},
	), nil
}

func (application *wiring) newFolderPicker() infrastructure.FolderPicker {
	return infrastructure.NewFolderPicker(filepath.Join(application.settings.paths.AppData, applicationFolder))
}

func (application *wiring) watchCommand(ctx context.Context) (presentation.CommandController, error) {
	stack, err := application.newImportStack(ctx)
	if err != nil {
		return nil, err
	}
	presenter := presentation.NewWatchPresenter(application.settings.console.output, application.errors)
	watch := services.NewWatchService(
		infrastructure.NewFolderWatcher(application.clock), stack.importer,
		infrastructure.NewFolderArchiver(), infrastructure.NewFolderStability(application.clock), presenter,
	)
	config := application.settings.config.Watch
	defaults := presentation.WatchDefaults{Inbox: config.Inbox, QuietPeriod: config.Settle, Import: stack.defaults}
	return presentation.NewWatchController(watch, presenter, defaults), nil
}
