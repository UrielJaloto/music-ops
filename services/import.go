package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/UrielJaloto/music-ops/domain"
)

var (
	ErrOutputInsideInput     = errors.New("output folder must not be inside the input folder")
	ErrFolderChoiceCancelled = errors.New("folder choice was cancelled")
)

const (
	StageDiscover        = "discover"
	StageConvert         = "convert"
	StageMetadata        = "metadata"
	StagePublish         = "publish"
	stageNumbering       = "numbering"
	stageTarget          = "target"
	parallelImportTasks  = 2
	convertTaskIndex     = 0
	skippedNotAudio      = "not an audio file"
	skippedPublished     = "already published"
	RowStatusPlanned     = "planned"
	importPathSeparator  = "/"
	windowsPathDelimiter = "\\"
)

var trackStatusNames = map[domain.TrackStatus]string{
	domain.StatusDiscovered: "discovered",
	domain.StatusConverted:  "converted",
	domain.StatusResolved:   "resolved",
	domain.StatusReady:      "ready",
	domain.StatusPublished:  "published",
	domain.StatusDuplicate:  "duplicate",
	domain.StatusSkipped:    "skipped",
	domain.StatusFailed:     "failed",
}

type SourceDiscoverer interface {
	Discover(ctx context.Context, input DiscoverInput) (DiscoveredSources, error)
}

type TrackConverter interface {
	Convert(ctx context.Context, input ConvertInput) (ConvertOutput, error)
}

type MetadataResolver interface {
	Resolve(ctx context.Context, input ResolveMetadataInput) (ResolvedMetadata, error)
}

type TrackPublisher interface {
	FindPublished(ctx context.Context, input FindPublishedInput) (PublishedTracks, error)
	Admit(ctx context.Context, input AdmitInput) (AdmittedTracks, error)
	Publish(ctx context.Context, input PublishInput) (PublishOutput, error)
}

type ImportProgress interface {
	StageStarted(stage string, total int)
	TrackFinished(stage, path, status string)
	StageFinished(stage string)
}

type ImportRuntime interface {
	RunParallel(ctx context.Context, size int, task func(ctx context.Context, index int) error) error
	Now() time.Time
	NewRunID(startedAt time.Time) (domain.RunID, error)
	Progress() ImportProgress
}

type ImportInput struct {
	Folder        string
	Output        string
	Profile       domain.EncodingProfile
	Numbering     domain.NumberingMode
	Layout        domain.Layout
	AlbumTemplate string
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
	Counts     domain.RunCounts
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
	resolved, err := service.resolver.Resolve(ctx, session.metadataInput(input.Numbering))
	if err != nil {
		return ImportPreviewOutput{}, fmt.Errorf("resolving metadata: %w", err)
	}
	session.applyMetadata(resolved, nopProgress{})
	targets := session.planTargets(input)
	return ImportPreviewOutput{Rows: session.rows(targets, RowStatusPlanned), Warnings: session.run.Warnings()}, nil
}

func (service ImportService) start(ctx context.Context, input ImportInput) (*importSession, error) {
	if err := validateImportFolders(input); err != nil {
		return nil, err
	}
	startedAt := service.runtime.Now()
	progress := service.runtime.Progress()
	progress.StageStarted(StageDiscover, 0)
	discovered, err := service.sources.Discover(ctx, DiscoverInput{Folder: input.Folder})
	if err != nil {
		return nil, fmt.Errorf("discovering sources: %w", err)
	}
	progress.StageFinished(StageDiscover)
	run, err := service.newRun(input, startedAt)
	if err != nil {
		return nil, err
	}
	session := newImportSession(run, discovered)
	session.recordTiming(StageDiscover, service.runtime.Now().Sub(startedAt))
	return session, nil
}

func (service ImportService) newRun(input ImportInput, startedAt time.Time) (*domain.Run, error) {
	id, err := service.runtime.NewRunID(startedAt)
	if err != nil {
		return nil, fmt.Errorf("creating run id: %w", err)
	}
	run, err := domain.NewRun(domain.RunDetails{ID: id, Input: input.Folder, Output: input.Output, StartedAt: startedAt})
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func validateImportFolders(input ImportInput) error {
	folder := normalizedFolder(input.Folder)
	output := normalizedFolder(input.Output)
	if folder == "" || output == "" {
		return ErrFolderMissing
	}
	if output == folder || strings.HasPrefix(output, folder+importPathSeparator) {
		return ErrOutputInsideInput
	}
	return nil
}

func normalizedFolder(folder string) string {
	unified := strings.ReplaceAll(strings.TrimSpace(folder), windowsPathDelimiter, importPathSeparator)
	return strings.ToLower(strings.TrimRight(unified, importPathSeparator))
}

func (service ImportService) skipPublished(ctx context.Context, session *importSession, output string) error {
	pending := session.pendingTracks()
	ids := make([]domain.TrackID, 0, len(pending))
	for _, track := range pending {
		ids = append(ids, track.ID())
	}
	published, err := service.publisher.FindPublished(ctx, FindPublishedInput{Output: output, IDs: ids})
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
	converted ConvertOutput
	resolved  ResolvedMetadata
}

func (service ImportService) convertAndResolve(ctx context.Context, session *importSession, input ImportInput) error {
	startedAt := service.runtime.Now()
	var results importResults
	err := service.runtime.RunParallel(ctx, parallelImportTasks, func(ctx context.Context, index int) error {
		if index == convertTaskIndex {
			return service.convertTracks(ctx, session.convertInput(input.Profile), &results.converted)
		}
		return service.resolveTracks(ctx, session.metadataInput(input.Numbering), &results.resolved)
	})
	if err != nil {
		return err
	}
	progress := service.runtime.Progress()
	session.applyConversions(results.converted, progress)
	session.applyMetadata(results.resolved, progress)
	elapsed := service.runtime.Now().Sub(startedAt)
	session.recordTiming(StageConvert, elapsed)
	session.recordTiming(StageMetadata, elapsed)
	return nil
}

func (service ImportService) convertTracks(ctx context.Context, input ConvertInput, output *ConvertOutput) error {
	converted, err := service.converter.Convert(ctx, input)
	if err != nil {
		return fmt.Errorf("converting tracks: %w", err)
	}
	*output = converted
	return nil
}

func (service ImportService) resolveTracks(ctx context.Context, input ResolveMetadataInput, output *ResolvedMetadata) error {
	resolved, err := service.resolver.Resolve(ctx, input)
	if err != nil {
		return fmt.Errorf("resolving metadata: %w", err)
	}
	*output = resolved
	return nil
}

func (service ImportService) publish(ctx context.Context, session *importSession, input ImportInput) error {
	startedAt := service.runtime.Now()
	admitted, err := service.publisher.Admit(ctx, AdmitInput{Output: input.Output, Tracks: session.admitTracks()})
	if err != nil {
		return fmt.Errorf("admitting tracks: %w", err)
	}
	session.applyDuplicates(admitted.Duplicates)
	requests := session.publishTracks(admitted.Publishable, input.Profile)
	progress := service.runtime.Progress()
	progress.StageStarted(StagePublish, len(requests))
	outputs := make([]PublishOutput, len(requests))
	err = service.runtime.RunParallel(ctx, len(requests), func(ctx context.Context, index int) error {
		published, err := service.publisher.Publish(ctx, PublishInput{Track: requests[index], Output: input.Output})
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

type nopProgress struct{}

func (nopProgress) StageStarted(string, int)             {}
func (nopProgress) TrackFinished(string, string, string) {}
func (nopProgress) StageFinished(string)                 {}

type importSession struct {
	run         *domain.Run
	tracks      []*domain.Track
	byID        map[domain.TrackID]*domain.Track
	conversions map[domain.TrackID]ConvertedTrack
	timings     []StageTiming
}

func newImportSession(run *domain.Run, discovered DiscoveredSources) *importSession {
	session := &importSession{run: run, byID: map[domain.TrackID]*domain.Track{}, conversions: map[domain.TrackID]ConvertedTrack{}, timings: []StageTiming{}}
	for _, source := range discovered.Tracks {
		session.addTrack(source.ID, source.Source, func(*domain.Track) error { return nil })
	}
	for _, failure := range discovered.Failed {
		session.addTrack(placeholderID(failure.Path), domain.SourceDetails{Path: failure.Path}, func(track *domain.Track) error {
			return track.Fail(StageDiscover, failure.Reason)
		})
	}
	for _, ignored := range discovered.Ignored {
		session.addTrack(placeholderID(ignored), domain.SourceDetails{Path: ignored}, func(track *domain.Track) error {
			return track.Skip(skippedNotAudio)
		})
	}
	run.AddTracks(session.tracks)
	return session
}

func placeholderID(path string) domain.TrackID {
	return domain.NewTrackID(path, 0, time.Time{})
}

func (session *importSession) addTrack(id domain.TrackID, source domain.SourceDetails, prepare func(*domain.Track) error) {
	track, err := domain.NewTrack(id, source)
	if err != nil {
		session.run.Warn(fmt.Sprintf("ignored source %q: %v", source.Path, err))
		return
	}
	if err := prepare(&track); err != nil {
		session.run.Warn(fmt.Sprintf("ignored source %q: %v", source.Path, err))
		return
	}
	session.tracks = append(session.tracks, &track)
	session.byID[id] = &track
}

func (session *importSession) recordTiming(stage string, duration time.Duration) {
	session.timings = append(session.timings, StageTiming{Stage: stage, Duration: duration})
}

func (session *importSession) pendingTracks() []*domain.Track {
	pending := []*domain.Track{}
	for _, track := range session.tracks {
		if track.Status() == domain.StatusDiscovered {
			pending = append(pending, track)
		}
	}
	return pending
}

func (session *importSession) convertInput(profile domain.EncodingProfile) ConvertInput {
	pending := session.pendingTracks()
	tracks := make([]ConvertTrack, 0, len(pending))
	for _, track := range pending {
		source := track.Source()
		tracks = append(tracks, ConvertTrack{ID: track.ID(), Source: source.Path, Codec: source.Codec, Quality: source.Quality})
	}
	return ConvertInput{Tracks: tracks, Profile: profile}
}

func (session *importSession) metadataInput(numbering domain.NumberingMode) ResolveMetadataInput {
	pending := session.pendingTracks()
	tracks := make([]MetadataTrack, 0, len(pending))
	for _, track := range pending {
		tracks = append(tracks, MetadataTrack{ID: track.ID(), Source: track.Source()})
	}
	return ResolveMetadataInput{Tracks: tracks, Numbering: numbering}
}

func (session *importSession) applyConversions(output ConvertOutput, progress ImportProgress) {
	progress.StageStarted(StageConvert, len(output.Converted)+len(output.Failed))
	for _, converted := range output.Converted {
		session.conversions[converted.ID] = converted
		session.transition(converted.ID, StageConvert, progress, func(track *domain.Track) error { return track.MarkConverted() })
	}
	for _, failure := range output.Failed {
		session.transition(failure.ID, StageConvert, progress, func(track *domain.Track) error { return track.Fail(StageConvert, failure.Reason) })
	}
	progress.StageFinished(StageConvert)
}

func (session *importSession) applyMetadata(resolved ResolvedMetadata, progress ImportProgress) {
	progress.StageStarted(StageMetadata, len(resolved.Tracks)+len(resolved.Failed))
	for _, result := range resolved.Tracks {
		session.transition(result.ID, StageMetadata, progress, func(track *domain.Track) error {
			return track.Resolve(result.Credit, result.Classification)
		})
	}
	for _, failure := range resolved.Failed {
		session.transition(failure.ID, StageMetadata, progress, func(track *domain.Track) error { return track.Fail(StageMetadata, failure.Reason) })
	}
	for _, warning := range resolved.Warnings {
		session.run.Warn(warning)
	}
	progress.StageFinished(StageMetadata)
}

func (session *importSession) transition(id domain.TrackID, stage string, progress ImportProgress, change func(*domain.Track) error) {
	track, found := session.byID[id]
	if !found || track.IsTerminal() {
		return
	}
	if err := change(track); err != nil {
		session.run.Warn(fmt.Sprintf("%s: %s: %v", stage, track.Source().Path, err))
		return
	}
	progress.TrackFinished(stage, track.Source().Path, trackStatusNames[track.Status()])
}

func (session *importSession) planTargets(input ImportInput) map[domain.TrackID]domain.TargetPath {
	resolved := session.resolvedTracks()
	for _, group := range domain.GroupByDirectory(resolved) {
		if err := group.Number(input.Numbering); err != nil {
			session.failGroup(group, err)
		}
	}
	targets := map[domain.TrackID]domain.TargetPath{}
	for _, track := range resolved {
		if track.IsTerminal() {
			continue
		}
		target, err := targetFor(track, input)
		if err != nil {
			session.failTrack(track, stageTarget, err)
			continue
		}
		targets[track.ID()] = target
	}
	return targets
}

func (session *importSession) resolvedTracks() []*domain.Track {
	resolved := []*domain.Track{}
	for _, track := range session.tracks {
		if track.Status() == domain.StatusResolved || track.Status() == domain.StatusReady {
			resolved = append(resolved, track)
		}
	}
	return resolved
}

func (session *importSession) skipTrack(track *domain.Track, reason string) {
	if err := track.Skip(reason); err != nil {
		session.run.Warn(fmt.Sprintf("skip: %s: %v", track.Source().Path, err))
	}
}

func (session *importSession) failGroup(group domain.AlbumGroup, err error) {
	for _, track := range group.Tracks() {
		session.failTrack(track, stageNumbering, err)
	}
}

func (session *importSession) failTrack(track *domain.Track, stage string, err error) {
	if track.IsTerminal() {
		return
	}
	if failErr := track.Fail(stage, err.Error()); failErr != nil {
		session.run.Warn(fmt.Sprintf("%s: %s: %v", stage, track.Source().Path, failErr))
	}
}

func targetFor(track *domain.Track, input ImportInput) (domain.TargetPath, error) {
	tags := track.Source().Tags
	return domain.NewTargetPath(domain.TargetPathDetails{
		Layout:      input.Layout,
		Template:    input.AlbumTemplate,
		AlbumArtist: track.Credit().Artists().MainArtist(),
		Album:       tags.Album,
		Date:        tags.Date,
		FileName:    domain.NewFileName(track.Credit()),
	})
}

func (session *importSession) assignTargets(targets map[domain.TrackID]domain.TargetPath) {
	for id, target := range targets {
		track := session.byID[id]
		if err := track.AssignTarget(target); err != nil {
			session.failTrack(track, stageTarget, err)
		}
	}
}

func (session *importSession) admitTracks() []AdmitTrack {
	admit := []AdmitTrack{}
	for _, track := range session.tracks {
		if track.CanPublish() {
			admit = append(admit, AdmitTrack{ID: track.ID(), Source: track.Source(), Target: track.Target()})
		}
	}
	return admit
}

func (session *importSession) applyDuplicates(duplicates []DuplicateTrack) {
	for _, duplicate := range duplicates {
		track, found := session.byID[duplicate.ID]
		if !found {
			continue
		}
		if err := track.MarkDuplicate(duplicate.KeeperPath); err != nil {
			session.run.Warn(fmt.Sprintf("duplicate %s: %v", track.Source().Path, err))
		}
	}
}

func (session *importSession) publishTracks(ids []domain.TrackID, profile domain.EncodingProfile) []PublishTrack {
	requests := make([]PublishTrack, 0, len(ids))
	for _, id := range ids {
		track, found := session.byID[id]
		if !found || !track.CanPublish() {
			continue
		}
		requests = append(requests, session.publishTrack(track, profile))
	}
	return requests
}

func (session *importSession) publishTrack(track *domain.Track, profile domain.EncodingProfile) PublishTrack {
	source := track.Source()
	conversion := session.conversions[track.ID()]
	return PublishTrack{
		ID:          track.ID(),
		StagedAudio: conversion.StagedAudio,
		FinalStaged: conversion.FinalStaged,
		CoverSource: source.Path,
		Cover:       source.Cover,
		Target:      track.Target().Relative(),
		Metadata:    track.Metadata(),
		Duration:    source.Quality.Duration(),
		Bitrate:     profile.Bitrate(),
		IsReencoded: domain.DecideEncoding(source.Codec) == domain.DecisionEncode,
	}
}

func (session *importSession) applyPublished(requests []PublishTrack, outputs []PublishOutput, progress ImportProgress) {
	for index, request := range requests {
		output := outputs[index]
		if output.Status == "" {
			continue
		}
		session.transition(request.ID, StagePublish, progress, func(track *domain.Track) error {
			if output.Status == PublishStatusFailed {
				return track.Fail(StagePublish+":"+output.Stage, output.Reason)
			}
			return track.MarkPublished()
		})
	}
}

func (session *importSession) toImportOutput() ImportOutput {
	return ImportOutput{
		RunID:      string(session.run.ID()),
		Input:      session.run.Input(),
		Output:     session.run.Output(),
		Counts:     session.run.Counts(),
		ExitStatus: session.run.ExitStatus(),
		Rows:       session.rows(nil, ""),
		Warnings:   session.run.Warnings(),
		Timings:    session.timings,
		Duration:   session.run.Duration(),
	}
}

func (session *importSession) rows(planned map[domain.TrackID]domain.TargetPath, plannedStatus string) []ImportRow {
	rows := make([]ImportRow, 0, len(session.tracks))
	for _, track := range session.tracks {
		rows = append(rows, toImportRow(track, planned, plannedStatus))
	}
	return rows
}

func toImportRow(track *domain.Track, planned map[domain.TrackID]domain.TargetPath, plannedStatus string) ImportRow {
	metadata := track.Metadata()
	row := ImportRow{
		Source: track.Source().Path,
		Target: track.Target().Relative(),
		Title:  metadata.Title,
		Artist: metadata.Artist,
		Genre:  metadata.Genre,
		Status: trackStatusNames[track.Status()],
		Reason: rowReason(track),
	}
	if target, isPlanned := planned[track.ID()]; isPlanned {
		row.Target = target.Relative()
		row.Status = plannedStatus
	}
	return row
}

func rowReason(track *domain.Track) string {
	outcome := track.Outcome()
	if outcome.KeeperPath != "" {
		return outcome.KeeperPath
	}
	if outcome.FailureStage != "" {
		return outcome.FailureStage + ": " + outcome.Reason
	}
	return outcome.Reason
}
