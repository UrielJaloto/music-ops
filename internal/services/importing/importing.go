package importing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/UrielJaloto/music-ops/internal/domain/albums"
	"github.com/UrielJaloto/music-ops/internal/domain/audio"
	"github.com/UrielJaloto/music-ops/internal/domain/filename"
	"github.com/UrielJaloto/music-ops/internal/domain/runs"
	"github.com/UrielJaloto/music-ops/internal/domain/tracks"
	"github.com/UrielJaloto/music-ops/internal/services/conversion"
	"github.com/UrielJaloto/music-ops/internal/services/discovery"
	"github.com/UrielJaloto/music-ops/internal/services/metadata"
	"github.com/UrielJaloto/music-ops/internal/services/publishing"
)

var (
	ErrFolderChoiceCancelled = errors.New("folder choice was cancelled")
	ErrWorkspaceClosed       = errors.New("the run workspace is not open")
	ErrRunInProgress         = errors.New("another music-ops run is in progress")
)

const (
	StageDiscover       = "discover"
	StageConvert        = "convert"
	StageMetadata       = "metadata"
	StagePublish        = "publish"
	stageNumbering      = "numbering"
	stageTarget         = "target"
	stageSkip           = "skip"
	stageDuplicate      = "duplicate"
	parallelImportTasks = 2
	convertTaskIndex    = 0
	skippedNotAudio     = "not an audio file"
	skippedPublished    = "already published"
	RowStatusPlanned    = "planned"
)

var trackStatusNames = map[tracks.TrackStatus]string{
	tracks.StatusDiscovered: "discovered",
	tracks.StatusConverted:  "converted",
	tracks.StatusResolved:   "resolved",
	tracks.StatusReady:      "ready",
	tracks.StatusPublished:  "published",
	tracks.StatusDuplicate:  "duplicate",
	tracks.StatusSkipped:    "skipped",
	tracks.StatusFailed:     "failed",
}

type SourceDiscoverer interface {
	Discover(ctx context.Context, input discovery.DiscoverInput) (discovery.DiscoveredSources, error)
}

type TrackConverter interface {
	Convert(ctx context.Context, input conversion.ConvertInput) (conversion.ConvertOutput, error)
}

type MetadataResolver interface {
	Resolve(ctx context.Context, input metadata.ResolveMetadataInput) (metadata.ResolvedMetadata, error)
}

type TrackPublisher interface {
	FindPublished(ctx context.Context, input publishing.FindPublishedInput) (publishing.PublishedTracks, error)
	Admit(ctx context.Context, input publishing.AdmitInput) (publishing.AdmittedTracks, error)
	Publish(ctx context.Context, input publishing.PublishInput) (publishing.PublishOutput, error)
}

type ImportProgress interface {
	StageStarted(stage string, total int)
	TrackFinished(stage, path, status string)
	StageFinished(stage string)
}

type ImportRuntime interface {
	RunParallel(ctx context.Context, size int, task func(ctx context.Context, index int) error) error
	Now() time.Time
	NewRunID(startedAt time.Time) (runs.RunID, error)
	Progress() ImportProgress
}

type ImportInput struct {
	Folder        string
	Output        string
	Profile       audio.EncodingProfile
	Numbering     albums.NumberingMode
	Layout        filename.Layout
	AlbumTemplate string
	Tags          tracks.TagOverrides
}

type ImportRow struct {
	Source string
	Target string
	Title  string
	Artist string
	Genre  string
	Status string
	Reason string
}

type StageTiming struct {
	Stage    string
	Duration time.Duration
}

type ImportOutput struct {
	RunID      string
	Input      string
	Output     string
	Counts     runs.RunCounts
	ExitStatus int
	Rows       []ImportRow
	Warnings   []string
	Timings    []StageTiming
	Duration   time.Duration
}

type ImportPreviewOutput struct {
	Rows     []ImportRow
	Warnings []string
}

type ImportService struct {
	sources   SourceDiscoverer
	converter TrackConverter
	resolver  MetadataResolver
	publisher TrackPublisher
	runtime   ImportRuntime
}

func NewImportService(sources SourceDiscoverer, converter TrackConverter, resolver MetadataResolver, publisher TrackPublisher, runtime ImportRuntime) ImportService {
	return ImportService{sources: sources, converter: converter, resolver: resolver, publisher: publisher, runtime: runtime}
}

func (service ImportService) Import(ctx context.Context, input ImportInput) (ImportOutput, error) {
	session, err := service.start(ctx, input)
	if err != nil {
		return ImportOutput{}, err
	}
	if err := service.skipPublished(ctx, session, input.Output); err != nil {
		return ImportOutput{}, err
	}
	if err := service.convertAndResolve(ctx, session, input); err != nil {
		return ImportOutput{}, err
	}
	session.assignTargets(session.planTargets(input))
	if err := service.publish(ctx, session, input); err != nil {
		return ImportOutput{}, err
	}
	if err := session.run.Finish(service.runtime.Now()); err != nil {
		return ImportOutput{}, fmt.Errorf("finishing run: %w", err)
	}
	return session.toImportOutput(), nil
}

func (service ImportService) Preview(ctx context.Context, input ImportInput) (ImportPreviewOutput, error) {
	session, err := service.start(ctx, input)
	if err != nil {
		return ImportPreviewOutput{}, err
	}
	resolved, err := service.resolver.Resolve(ctx, session.buildMetadataInput(input))
	if err != nil {
		return ImportPreviewOutput{}, fmt.Errorf("resolving metadata: %w", err)
	}
	session.applyMetadata(resolved, nopProgress{})
	targets := session.planTargets(input)
	return ImportPreviewOutput{Rows: session.buildRows(targets, RowStatusPlanned), Warnings: session.run.Warnings()}, nil
}

func (service ImportService) start(ctx context.Context, input ImportInput) (*importSession, error) {
	if err := validateImportFolders(input); err != nil {
		return nil, err
	}
	startedAt := service.runtime.Now()
	progress := service.runtime.Progress()
	progress.StageStarted(StageDiscover, 0)
	discovered, err := service.sources.Discover(ctx, discovery.DiscoverInput{Folder: input.Folder, Output: input.Output, Progress: stageCounter{stage: StageDiscover, done: tracks.StatusDiscovered, progress: progress}})
	if err != nil {
		return nil, fmt.Errorf("discovering sources: %w", err)
	}
	progress.StageFinished(StageDiscover)
	run, err := service.createRun(input, startedAt)
	if err != nil {
		return nil, err
	}
	session := newImportSession(run, discovered, input.Tags)
	session.recordTiming(StageDiscover, service.runtime.Now().Sub(startedAt))
	return session, nil
}

func (service ImportService) createRun(input ImportInput, startedAt time.Time) (*runs.Run, error) {
	id, err := service.runtime.NewRunID(startedAt)
	if err != nil {
		return nil, fmt.Errorf("creating run id: %w", err)
	}
	run, err := runs.NewRun(runs.RunDetails{ID: id, Input: input.Folder, Output: input.Output, StartedAt: startedAt})
	return &run, err
}

func validateImportFolders(input ImportInput) error {
	if strings.TrimSpace(input.Folder) == "" || strings.TrimSpace(input.Output) == "" {
		return discovery.ErrFolderMissing
	}
	return nil
}

func (service ImportService) skipPublished(ctx context.Context, session *importSession, output string) error {
	pending := session.listPendingTracks()
	ids := make([]tracks.TrackID, 0, len(pending))
	for _, track := range pending {
		ids = append(ids, track.ID())
	}
	published, err := service.publisher.FindPublished(ctx, publishing.FindPublishedInput{Output: output, IDs: ids})
	if err != nil {
		return fmt.Errorf("finding published tracks: %w", err)
	}
	for _, track := range pending {
		if _, isPublished := published.Targets[track.ID()]; isPublished {
			session.skipTrack(track, skippedPublished)
		}
	}
	return nil
}

type importResults struct {
	converted conversion.ConvertOutput
	resolved  metadata.ResolvedMetadata
}

func (service ImportService) convertAndResolve(ctx context.Context, session *importSession, input ImportInput) error {
	startedAt := service.runtime.Now()
	var results importResults
	progress := service.runtime.Progress()
	convertInput := session.buildConvertInput(input.Profile, stageCounter{stage: StageConvert, done: tracks.StatusConverted, progress: progress})
	metadataInput := session.buildMetadataInput(input)
	progress.StageStarted(StageConvert, len(convertInput.Tracks))
	err := service.runtime.RunParallel(ctx, parallelImportTasks, func(ctx context.Context, index int) error {
		if index != convertTaskIndex {
			return service.resolveTracks(ctx, metadataInput, &results.resolved)
		}
		err := service.convertTracks(ctx, convertInput, &results.converted)
		progress.StageFinished(StageConvert)
		progress.StageStarted(StageMetadata, len(metadataInput.Tracks))
		return err
	})
	if err != nil {
		return err
	}
	session.applyConversions(results.converted)
	session.applyMetadata(results.resolved, progress)
	elapsed := service.runtime.Now().Sub(startedAt)
	session.recordTiming(StageConvert, elapsed)
	session.recordTiming(StageMetadata, elapsed)
	return nil
}

func (service ImportService) convertTracks(ctx context.Context, input conversion.ConvertInput, output *conversion.ConvertOutput) error {
	converted, err := service.converter.Convert(ctx, input)
	if err != nil {
		return fmt.Errorf("converting tracks: %w", err)
	}
	*output = converted
	return nil
}

func (service ImportService) resolveTracks(ctx context.Context, input metadata.ResolveMetadataInput, output *metadata.ResolvedMetadata) error {
	resolved, err := service.resolver.Resolve(ctx, input)
	if err != nil {
		return fmt.Errorf("resolving metadata: %w", err)
	}
	*output = resolved
	return nil
}

func (service ImportService) publish(ctx context.Context, session *importSession, input ImportInput) error {
	startedAt := service.runtime.Now()
	admitted, err := service.publisher.Admit(ctx, publishing.AdmitInput{Output: input.Output, Tracks: session.admitTracks()})
	if err != nil {
		return fmt.Errorf("admitting tracks: %w", err)
	}
	session.applyDuplicates(admitted.Duplicates)
	requests := session.publishTracks(admitted.Publishable, input.Profile)
	progress := service.runtime.Progress()
	progress.StageStarted(StagePublish, len(requests))
	outputs := make([]publishing.PublishOutput, len(requests))
	err = service.runtime.RunParallel(ctx, len(requests), func(ctx context.Context, index int) error {
		published, err := service.publisher.Publish(ctx, publishing.PublishInput{Track: requests[index], Output: input.Output})
		outputs[index] = published
		return err
	})
	session.applyPublished(requests, outputs, progress)
	progress.StageFinished(StagePublish)
	session.recordTiming(StagePublish, service.runtime.Now().Sub(startedAt))
	if err != nil {
		return fmt.Errorf("publishing tracks: %w", err)
	}
	return nil
}

type stageCounter struct {
	stage    string
	done     tracks.TrackStatus
	progress ImportProgress
}

func (counter stageCounter) Counted(total int) {
	counter.progress.StageStarted(counter.stage, total)
}

func (counter stageCounter) Finished(path string, isFailed bool) {
	status := counter.done
	if isFailed {
		status = tracks.StatusFailed
	}
	counter.progress.TrackFinished(counter.stage, path, trackStatusNames[status])
}

type nopProgress struct{}

func (nopProgress) StageStarted(string, int)             {}
func (nopProgress) TrackFinished(string, string, string) {}
func (nopProgress) StageFinished(string)                 {}
