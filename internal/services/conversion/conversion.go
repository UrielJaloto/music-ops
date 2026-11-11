package conversion

import (
	"context"
	"fmt"

	"github.com/UrielJaloto/music-ops/internal/domain/audio"
	"github.com/UrielJaloto/music-ops/internal/domain/tracks"
)

const (
	StagedAudioSuffix = ".audio.m4a"
	FinalStagedSuffix = ".final.m4a"
)

type EncodeRequest struct {
	Source      string
	Destination string
	Profile     audio.EncodingProfile
	Quality     audio.AudioQuality
}

type AudioEncoder interface {
	Encode(ctx context.Context, request EncodeRequest) error
}

type AudioCopier interface {
	CopyAudio(ctx context.Context, source, destination string) error
}

type StagingArea interface {
	BuildStagedPath(id tracks.TrackID, suffix string) string
	ShortenSourcePath(ctx context.Context, source string) (string, error)
	Exists(path string) bool
}

type ConversionJournal interface {
	IsConverted(ctx context.Context, id tracks.TrackID) (bool, error)
	RecordConverted(ctx context.Context, id tracks.TrackID, staged string) error
}

type ConversionWorkPool interface {
	Run(ctx context.Context, size int, task func(ctx context.Context, index int) error) error
}

type ConvertTrack struct {
	ID      tracks.TrackID
	Source  string
	Codec   audio.Codec
	Quality audio.AudioQuality
}

type ConversionProgress interface {
	Finished(path string, isFailed bool)
}

type ConvertInput struct {
	Tracks   []ConvertTrack
	Profile  audio.EncodingProfile
	Progress ConversionProgress
}

type ConvertedTrack struct {
	ID          tracks.TrackID
	StagedAudio string
	FinalStaged string
}

type ConversionFailure struct {
	ID     tracks.TrackID
	Source string
	Reason string
}

type ConvertOutput struct {
	Converted []ConvertedTrack
	Failed    []ConversionFailure
}

type ConversionService struct {
	encoder AudioEncoder
	copier  AudioCopier
	staging StagingArea
	journal ConversionJournal
	pool    ConversionWorkPool
}

func NewConversionService(encoder AudioEncoder, copier AudioCopier, staging StagingArea, journal ConversionJournal, pool ConversionWorkPool) ConversionService {
	return ConversionService{encoder: encoder, copier: copier, staging: staging, journal: journal, pool: pool}
}

func (service ConversionService) Convert(ctx context.Context, input ConvertInput) (ConvertOutput, error) {
	errorsByTrack := make([]error, len(input.Tracks))
	err := service.pool.Run(ctx, len(input.Tracks), func(ctx context.Context, index int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		errorsByTrack[index] = service.convertTrack(ctx, input.Tracks[index], input.Profile)
		input.Progress.Finished(input.Tracks[index].Source, errorsByTrack[index] != nil)
		return nil
	})
	if err != nil {
		return ConvertOutput{}, fmt.Errorf("converting tracks: %w", err)
	}
	return service.collectConversions(input.Tracks, errorsByTrack), nil
}

func (service ConversionService) convertTrack(ctx context.Context, track ConvertTrack, profile audio.EncodingProfile) error {
	staged := service.staging.BuildStagedPath(track.ID, StagedAudioSuffix)
	isConverted, err := service.journal.IsConverted(ctx, track.ID)
	if err != nil {
		return fmt.Errorf("reading conversion journal: %w", err)
	}
	if isConverted && service.staging.Exists(staged) {
		return nil
	}
	shortSource, err := service.staging.ShortenSourcePath(ctx, track.Source)
	if err != nil {
		return fmt.Errorf("preparing short source path: %w", err)
	}
	request := EncodeRequest{Source: shortSource, Destination: staged, Profile: profile, Quality: track.Quality}
	if err := service.writeStagedAudio(ctx, track.Codec, request); err != nil {
		return err
	}
	if err := service.journal.RecordConverted(ctx, track.ID, staged); err != nil {
		return fmt.Errorf("recording conversion: %w", err)
	}
	return nil
}

func (service ConversionService) writeStagedAudio(ctx context.Context, codec audio.Codec, request EncodeRequest) error {
	if audio.DecideEncoding(codec, request.Quality) == audio.DecisionCopy {
		if err := service.copier.CopyAudio(ctx, request.Source, request.Destination); err != nil {
			return fmt.Errorf("copying audio: %w", err)
		}
		return nil
	}
	if err := service.encoder.Encode(ctx, request); err != nil {
		return fmt.Errorf("encoding audio: %w", err)
	}
	return nil
}

func (service ConversionService) collectConversions(tracks []ConvertTrack, errorsByTrack []error) ConvertOutput {
	output := ConvertOutput{Converted: []ConvertedTrack{}, Failed: []ConversionFailure{}}
	for index, track := range tracks {
		if errorsByTrack[index] != nil {
			output.Failed = append(output.Failed, ConversionFailure{ID: track.ID, Source: track.Source, Reason: errorsByTrack[index].Error()})
			continue
		}
		output.Converted = append(output.Converted, ConvertedTrack{
			ID:          track.ID,
			StagedAudio: service.staging.BuildStagedPath(track.ID, StagedAudioSuffix),
			FinalStaged: service.staging.BuildStagedPath(track.ID, FinalStagedSuffix),
		})
	}
	return output
}
