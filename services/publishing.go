package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/UrielJaloto/music-ops/domain"
)

const (
	PublishStatusPublished        = "published"
	PublishStatusAlreadyPublished = "already published"
	PublishStatusFailed           = "failed"
	PublishStageJournal           = "journal"
	PublishStageFinalize          = "finalize"
	PublishStageMove              = "move"
	PublishStageVerify            = "verify"
	outputPathSeparator           = "/"
)

type LibraryReader interface {
	ListTrackPaths(ctx context.Context, root string) ([]string, error)
}

type FinalizeRequest struct {
	Staged      string
	CoverSource string
	Cover       domain.CoverArt
	Destination string
	Metadata    domain.TrackMetadata
}

type TrackFinalizer interface {
	Finalize(ctx context.Context, request FinalizeRequest) error
}

type TrackMover interface {
	Move(ctx context.Context, source, destination string) error
	Remove(ctx context.Context, path string) error
	Exists(path string) bool
}

type PublishedAudio struct {
	Metadata    domain.TrackMetadata
	Duration    time.Duration
	Bitrate     int
	IsReencoded bool
}

type PublishedAudioVerifier interface {
	Verify(ctx context.Context, path string, expected PublishedAudio) error
}

type PublishingJournal interface {
	IsPublished(ctx context.Context, id domain.TrackID) (string, bool, error)
	RecordPublished(ctx context.Context, id domain.TrackID, target string) error
}

type AdmitTrack struct {
	ID     domain.TrackID
	Source domain.SourceDetails
	Target domain.TargetPath
}

type AdmitInput struct {
	Output string
	Tracks []AdmitTrack
}

type DuplicateTrack struct {
	ID         domain.TrackID
	KeeperPath string
}

type AdmittedTracks struct {
	Publishable []domain.TrackID
	Duplicates  []DuplicateTrack
}

type PublishTrack struct {
	ID          domain.TrackID
	StagedAudio string
	FinalStaged string
	CoverSource string
	Cover       domain.CoverArt
	Target      string
	Metadata    domain.TrackMetadata
	Duration    time.Duration
	Bitrate     int
	IsReencoded bool
}

type PublishInput struct {
	Track  PublishTrack
	Output string
}

type PublishOutput struct {
	Status      string
	Stage       string
	Reason      string
	Destination string
}

type PublishingService struct {
	library   LibraryReader
	finalizer TrackFinalizer
	mover     TrackMover
	verifier  PublishedAudioVerifier
	journal   PublishingJournal
}

func NewPublishingService(library LibraryReader, finalizer TrackFinalizer, mover TrackMover, verifier PublishedAudioVerifier, journal PublishingJournal) PublishingService {
	return PublishingService{library: library, finalizer: finalizer, mover: mover, verifier: verifier, journal: journal}
}

func (service PublishingService) Admit(ctx context.Context, input AdmitInput) (AdmittedTracks, error) {
	existing, err := service.library.ListTrackPaths(ctx, input.Output)
	if err != nil {
		return AdmittedTracks{}, fmt.Errorf("listing library %s: %w", input.Output, err)
	}
	tracks, err := toAdmissionTracks(input.Tracks)
	if err != nil {
		return AdmittedTracks{}, err
	}
	if err := domain.NewLibrary(existing, input.Output).Admit(tracks); err != nil {
		return AdmittedTracks{}, fmt.Errorf("admitting tracks: %w", err)
	}
	return toAdmittedTracks(tracks), nil
}

func toAdmissionTracks(inputs []AdmitTrack) ([]*domain.Track, error) {
	tracks := make([]*domain.Track, 0, len(inputs))
	for _, input := range inputs {
		track, err := admissionTrack(input)
		if err != nil {
			return nil, fmt.Errorf("preparing %s for admission: %w", input.Source.Path, err)
		}
		tracks = append(tracks, track)
	}
	return tracks, nil
}

func admissionTrack(input AdmitTrack) (*domain.Track, error) {
	track, err := domain.NewTrack(input.ID, input.Source)
	if err != nil {
		return nil, err
	}
	steps := []func() error{
		track.MarkConverted,
		func() error { return track.Resolve(domain.Credit{}, domain.UnknownClassification()) },
		func() error { return track.AssignTarget(input.Target) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, err
		}
	}
	return &track, nil
}

func toAdmittedTracks(tracks []*domain.Track) AdmittedTracks {
	admitted := AdmittedTracks{Publishable: []domain.TrackID{}, Duplicates: []DuplicateTrack{}}
	for _, track := range tracks {
		if track.Status() == domain.StatusDuplicate {
			admitted.Duplicates = append(admitted.Duplicates, DuplicateTrack{ID: track.ID(), KeeperPath: track.Outcome().KeeperPath})
			continue
		}
		if !track.CanPublish() {
			continue
		}
		admitted.Publishable = append(admitted.Publishable, track.ID())
	}
	return admitted
}

func (service PublishingService) Publish(ctx context.Context, input PublishInput) (PublishOutput, error) {
	destination := strings.TrimRight(input.Output, "/\\") + outputPathSeparator + input.Track.Target
	_, isPublished, err := service.journal.IsPublished(ctx, input.Track.ID)
	if err != nil {
		return failedPublish(PublishStageJournal, err, destination), nil
	}
	if isPublished && service.mover.Exists(destination) {
		return PublishOutput{Status: PublishStatusAlreadyPublished, Destination: destination}, nil
	}
	if stage, err := service.writeTrack(ctx, input.Track, destination); err != nil {
		return failedPublish(stage, err, destination), nil
	}
	if err := service.journal.RecordPublished(ctx, input.Track.ID, input.Track.Target); err != nil {
		return failedPublish(PublishStageJournal, err, destination), nil
	}
	return PublishOutput{Status: PublishStatusPublished, Destination: destination}, nil
}

func (service PublishingService) writeTrack(ctx context.Context, track PublishTrack, destination string) (string, error) {
	request := FinalizeRequest{Staged: track.StagedAudio, CoverSource: track.CoverSource, Cover: track.Cover, Destination: track.FinalStaged, Metadata: track.Metadata}
	if err := service.finalizer.Finalize(ctx, request); err != nil {
		return PublishStageFinalize, err
	}
	if err := service.mover.Move(ctx, track.FinalStaged, destination); err != nil {
		return PublishStageMove, err
	}
	expected := PublishedAudio{Metadata: track.Metadata, Duration: track.Duration, Bitrate: track.Bitrate, IsReencoded: track.IsReencoded}
	if err := service.verifier.Verify(ctx, destination, expected); err != nil {
		return PublishStageVerify, service.removeInvalid(ctx, destination, err)
	}
	return "", nil
}

func (service PublishingService) removeInvalid(ctx context.Context, destination string, verifyErr error) error {
	if err := service.mover.Remove(ctx, destination); err != nil {
		return fmt.Errorf("%w (removing invalid file: %w)", verifyErr, err)
	}
	return verifyErr
}

func failedPublish(stage string, err error, destination string) PublishOutput {
	return PublishOutput{Status: PublishStatusFailed, Stage: stage, Reason: err.Error(), Destination: destination}
}
