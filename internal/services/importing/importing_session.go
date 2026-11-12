package importing

import (
	"fmt"
	"path/filepath"
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

type importSession struct {
	run         *runs.Run
	tracks      []*tracks.Track
	byID        map[tracks.TrackID]*tracks.Track
	conversions map[tracks.TrackID]conversion.ConvertedTrack
	timings     []StageTiming
}

func newImportSession(run *runs.Run, discovered discovery.DiscoveredSources, overrides tracks.TagOverrides) *importSession {
	session := &importSession{run: run, byID: map[tracks.TrackID]*tracks.Track{}, conversions: map[tracks.TrackID]conversion.ConvertedTrack{}, timings: []StageTiming{}}
	for _, source := range discovered.Tracks {
		details := source.Source
		details.Tags = overrides.Apply(details.Tags)
		session.addTrack(source.ID, details, func(*tracks.Track) error { return nil })
	}
	for _, failure := range discovered.Failed {
		session.addTrack(buildPlaceholderID(failure.Path), tracks.SourceDetails{Path: failure.Path}, func(track *tracks.Track) error {
			return track.Fail(StageDiscover, failure.Reason)
		})
	}
	for _, ignored := range discovered.Ignored {
		session.addTrack(buildPlaceholderID(ignored), tracks.SourceDetails{Path: ignored}, func(track *tracks.Track) error {
			return track.Skip(skippedNotAudio)
		})
	}
	run.AddTracks(session.tracks)
	return session
}

func buildPlaceholderID(path string) tracks.TrackID {
	return tracks.NewTrackID(path, 0, time.Time{})
}

func (session *importSession) addTrack(id tracks.TrackID, source tracks.SourceDetails, prepare func(*tracks.Track) error) {
	track, err := tracks.NewTrack(id, source)
	if err == nil {
		err = prepare(&track)
	}
	if err != nil {
		session.run.Warn(fmt.Sprintf("ignored source %q: %v", source.Path, err))
		return
	}
	session.tracks = append(session.tracks, &track)
	session.byID[id] = &track
}

func (session *importSession) recordTiming(stage string, duration time.Duration) {
	session.timings = append(session.timings, StageTiming{Stage: stage, Duration: duration})
}

func (session *importSession) listPendingTracks() []*tracks.Track {
	pending := []*tracks.Track{}
	for _, track := range session.tracks {
		if track.Status() == tracks.StatusDiscovered {
			pending = append(pending, track)
		}
	}
	return pending
}

func (session *importSession) buildConvertInput(profile audio.EncodingProfile, progress conversion.ConversionProgress) conversion.ConvertInput {
	pending := session.listPendingTracks()
	tracks := make([]conversion.ConvertTrack, 0, len(pending))
	for _, track := range pending {
		source := track.Source()
		tracks = append(tracks, conversion.ConvertTrack{ID: track.ID(), Source: source.Path, Codec: source.Codec, Quality: source.Quality})
	}
	return conversion.ConvertInput{Tracks: tracks, Profile: profile, Progress: progress}
}

func (session *importSession) buildMetadataInput(input ImportInput) metadata.ResolveMetadataInput {
	pending := session.listPendingTracks()
	tracks := make([]metadata.MetadataTrack, 0, len(pending))
	for _, track := range pending {
		tracks = append(tracks, metadata.MetadataTrack{ID: track.ID(), Source: track.Source()})
	}
	return metadata.ResolveMetadataInput{Tracks: tracks, Numbering: input.Numbering, FixedGenre: input.Tags.Genre}
}

func (session *importSession) applyConversions(output conversion.ConvertOutput) {
	progress := nopProgress{}
	for _, converted := range output.Converted {
		session.conversions[converted.ID] = converted
		session.applyTransition(converted.ID, stageProgress{stage: StageConvert, progress: progress}, func(track *tracks.Track) error { return track.MarkConverted() })
	}
	for _, failure := range output.Failed {
		session.applyTransition(failure.ID, stageProgress{stage: StageConvert, progress: progress}, func(track *tracks.Track) error { return track.Fail(StageConvert, failure.Reason) })
	}
}

func (session *importSession) applyMetadata(resolved metadata.ResolvedMetadata, progress ImportProgress) {
	for _, result := range resolved.Tracks {
		session.applyTransition(result.ID, stageProgress{stage: StageMetadata, progress: progress}, func(track *tracks.Track) error {
			return track.Resolve(result.Credit, result.Classification)
		})
	}
	for _, failure := range resolved.Failed {
		session.applyTransition(failure.ID, stageProgress{stage: StageMetadata, progress: progress}, func(track *tracks.Track) error { return track.Fail(StageMetadata, failure.Reason) })
	}
	for _, warning := range resolved.Warnings {
		session.run.Warn(warning)
	}
	progress.StageFinished(StageMetadata)
}

type stageProgress struct {
	stage    string
	progress ImportProgress
}

func (session *importSession) applyTransition(id tracks.TrackID, step stageProgress, change func(*tracks.Track) error) {
	stage, progress := step.stage, step.progress
	track, found := session.byID[id]
	if !found || track.IsTerminal() {
		return
	}
	if err := change(track); err != nil {
		session.warn(stage, track, err)
		return
	}
	progress.TrackFinished(stage, track.Source().Path, trackStatusNames[track.Status()])
}

func (session *importSession) planTargets(input ImportInput) map[tracks.TrackID]filename.TargetPath {
	resolved := session.listResolvedTracks()
	for _, group := range albums.GroupByDirectory(resolved) {
		if err := group.NumberTracks(input.Numbering); err != nil {
			session.failGroup(group, err)
		}
	}
	targets := map[tracks.TrackID]filename.TargetPath{}
	for _, track := range resolved {
		if track.IsTerminal() {
			continue
		}
		target, err := buildTarget(track, input)
		if err != nil {
			session.failTrack(track, stageTarget, err)
			continue
		}
		targets[track.ID()] = target
	}
	return targets
}

func (session *importSession) listResolvedTracks() []*tracks.Track {
	resolved := []*tracks.Track{}
	for _, track := range session.tracks {
		if track.Status() == tracks.StatusResolved || track.Status() == tracks.StatusReady {
			resolved = append(resolved, track)
		}
	}
	return resolved
}

func (session *importSession) skipTrack(track *tracks.Track, reason string) {
	session.warn(stageSkip, track, track.Skip(reason))
}

func (session *importSession) warn(stage string, track *tracks.Track, err error) {
	if err != nil {
		session.run.Warn(fmt.Sprintf("%s: %s: %v", stage, track.Source().Path, err))
	}
}

func (session *importSession) failGroup(group albums.AlbumGroup, err error) {
	for _, track := range group.Tracks() {
		session.failTrack(track, stageNumbering, err)
	}
}

func (session *importSession) failTrack(track *tracks.Track, stage string, err error) {
	session.warn(stage, track, track.Fail(stage, err.Error()))
}

func extractFolderName(folder string) string {
	return filepath.Base(filepath.Clean(folder))
}

func buildTarget(track *tracks.Track, input ImportInput) (filename.TargetPath, error) {
	tags := track.Source().Tags
	return filename.NewTargetPath(filename.TargetPathDetails{
		Folder:      extractFolderName(input.Folder),
		Layout:      input.Layout,
		Template:    input.AlbumTemplate,
		AlbumArtist: track.Credit().Artists().MainArtist(),
		Album:       tags.Album,
		Date:        tags.Date,
		FileName:    filename.NewFileName(track.Credit()),
	})
}

func (session *importSession) assignTargets(targets map[tracks.TrackID]filename.TargetPath) {
	for id, target := range targets {
		track := session.byID[id]
		if err := track.AssignTarget(target); err != nil {
			session.failTrack(track, stageTarget, err)
		}
	}
}

func (session *importSession) admitTracks() []publishing.AdmitTrack {
	admit := []publishing.AdmitTrack{}
	for _, track := range session.tracks {
		if track.CanPublish() {
			admit = append(admit, publishing.AdmitTrack{ID: track.ID(), Source: track.Source(), Target: track.Target()})
		}
	}
	return admit
}

func (session *importSession) applyDuplicates(duplicates []publishing.DuplicateTrack) {
	for _, duplicate := range duplicates {
		track, found := session.byID[duplicate.ID]
		if !found {
			continue
		}
		session.warn(stageDuplicate, track, track.MarkDuplicate(duplicate.KeeperPath))
	}
}

func (session *importSession) publishTracks(ids []tracks.TrackID, profile audio.EncodingProfile) []publishing.PublishTrack {
	requests := make([]publishing.PublishTrack, 0, len(ids))
	for _, id := range ids {
		track, found := session.byID[id]
		if !found || !track.CanPublish() {
			continue
		}
		requests = append(requests, session.publishTrack(track, profile))
	}
	return requests
}

func (session *importSession) publishTrack(track *tracks.Track, profile audio.EncodingProfile) publishing.PublishTrack {
	source := track.Source()
	conversion := session.conversions[track.ID()]
	return publishing.PublishTrack{
		ID:          track.ID(),
		StagedAudio: conversion.StagedAudio,
		FinalStaged: conversion.FinalStaged,
		CoverSource: source.Path,
		Cover:       source.Cover,
		Target:      track.Target().Relative(),
		Metadata:    track.BuildMetadata(),
		Duration:    source.Quality.Duration(),
		Bitrate:     profile.Bitrate(),
		IsReencoded: audio.DecideEncoding(source.Codec, source.Quality) == audio.DecisionEncode && profile.HasConstantBitrate(),
	}
}

func (session *importSession) applyPublished(requests []publishing.PublishTrack, outputs []publishing.PublishOutput, progress ImportProgress) {
	for index, request := range requests {
		output := outputs[index]
		if output.Status == "" {
			continue
		}
		session.applyTransition(request.ID, stageProgress{stage: StagePublish, progress: progress}, func(track *tracks.Track) error {
			if output.Status == publishing.PublishStatusFailed {
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
		ExitStatus: session.run.DecideExitStatus(),
		Rows:       session.buildRows(nil, ""),
		Warnings:   session.run.Warnings(),
		Timings:    session.timings,
		Duration:   session.run.Duration(),
	}
}

func (session *importSession) buildRows(planned map[tracks.TrackID]filename.TargetPath, plannedStatus string) []ImportRow {
	rows := make([]ImportRow, 0, len(session.tracks))
	for _, track := range session.tracks {
		rows = append(rows, toImportRow(track, planned, plannedStatus))
	}
	return rows
}

func toImportRow(track *tracks.Track, planned map[tracks.TrackID]filename.TargetPath, plannedStatus string) ImportRow {
	metadata := track.BuildMetadata()
	row := ImportRow{
		Source: track.Source().Path,
		Target: track.Target().Relative(),
		Title:  metadata.Title,
		Artist: metadata.Artist,
		Genre:  metadata.Genre,
		Status: trackStatusNames[track.Status()],
		Reason: describeOutcome(track),
	}
	if target, isPlanned := planned[track.ID()]; isPlanned {
		row.Target = target.Relative()
		row.Status = plannedStatus
	}
	return row
}

func describeOutcome(track *tracks.Track) string {
	outcome := track.Outcome()
	if outcome.KeeperPath != "" {
		return outcome.KeeperPath
	}
	if outcome.FailureStage != "" {
		return outcome.FailureStage + ": " + outcome.Reason
	}
	return outcome.Reason
}
