package publishing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/UrielJaloto/music-ops/internal/domain/credits"
	"github.com/UrielJaloto/music-ops/internal/domain/filename"
	"github.com/UrielJaloto/music-ops/internal/domain/genres"
	"github.com/UrielJaloto/music-ops/internal/domain/library"
	"github.com/UrielJaloto/music-ops/internal/domain/tracks"
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
	Cover       tracks.CoverArt
	Destination string
	Metadata    tracks.TrackMetadata
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
	Metadata    tracks.TrackMetadata
	Duration    time.Duration
	Bitrate     int
	IsReencoded bool
}

type PublishedAudioVerifier interface {
	Verify(ctx context.Context, path string, expected PublishedAudio) error
}

type PublishingJournal interface {
	IsPublished(ctx context.Context, id tracks.TrackID) (string, bool, error)
	RecordPublished(ctx context.Context, id tracks.TrackID, target string) error
}

type AdmitTrack struct {
	ID     tracks.TrackID
	Source tracks.SourceDetails
	Target filename.TargetPath
}

type AdmitInput struct {
	Output string
	Tracks []AdmitTrack
}

type DuplicateTrack struct {
	ID         tracks.TrackID
	KeeperPath string
}

type AdmittedTracks struct {
	Publishable []tracks.TrackID
	Duplicates  []DuplicateTrack
}

type PublishTrack struct {
	ID          tracks.TrackID
	StagedAudio string
	FinalStaged string
	CoverSource string
	Cover       tracks.CoverArt
	Target      string
	Metadata    tracks.TrackMetadata
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

type FindPublishedInput struct {
	Output string
	IDs    []tracks.TrackID
}

type PublishedTracks struct {
	Targets map[tracks.TrackID]string
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
	admitErr := library.NewLibrary(existing, input.Output).Admit(tracks)
	return toAdmittedTracks(tracks), admitErr
}

func toAdmissionTracks(inputs []AdmitTrack) ([]*tracks.Track, error) {
	tracks := make([]*tracks.Track, 0, len(inputs))
	for _, input := range inputs {
		track, err := buildAdmissionTrack(input)
		if err != nil {
			return nil, fmt.Errorf("preparing %s for admission: %w", input.Source.Path, err)
		}
		tracks = append(tracks, track)
	}
	return tracks, nil
}

func buildAdmissionTrack(input AdmitTrack) (*tracks.Track, error) {
	track, err := tracks.NewTrack(input.ID, input.Source)
	if err != nil {
		return nil, err
	}
	err = errors.Join(track.MarkConverted(), track.Resolve(credits.Credit{}, genres.NewUnknownClassification()), track.AssignTarget(input.Target))
	return &track, err
}

func toAdmittedTracks(candidates []*tracks.Track) AdmittedTracks {
	admitted := AdmittedTracks{Publishable: []tracks.TrackID{}, Duplicates: []DuplicateTrack{}}
	for _, track := range candidates {
		if track.Status() == tracks.StatusDuplicate {
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

func (service PublishingService) FindPublished(ctx context.Context, input FindPublishedInput) (PublishedTracks, error) {
	published := PublishedTracks{Targets: map[tracks.TrackID]string{}}
	for _, id := range input.IDs {
		target, isPublished, err := service.journal.IsPublished(ctx, id)
		if err != nil {
			return PublishedTracks{}, fmt.Errorf("reading publishing journal: %w", err)
		}
		if isPublished && service.mover.Exists(destinationOf(input.Output, target)) {
			published.Targets[id] = target
		}
	}
	return published, nil
}

func destinationOf(output, target string) string {
	return strings.TrimRight(output, "/\\") + outputPathSeparator + target
}

func (service PublishingService) Publish(ctx context.Context, input PublishInput) (PublishOutput, error) {
	destination := destinationOf(input.Output, input.Track.Target)
	_, isPublished, err := service.journal.IsPublished(ctx, input.Track.ID)
	if err != nil {
		return buildFailedPublish(PublishStageJournal, err, destination), nil
	}
	if isPublished && service.mover.Exists(destination) {
		return PublishOutput{Status: PublishStatusAlreadyPublished, Destination: destination}, nil
	}
	if stage, err := service.writeTrack(ctx, input.Track, destination); err != nil {
		return buildFailedPublish(stage, err, destination), nil
	}
	if err := service.journal.RecordPublished(ctx, input.Track.ID, input.Track.Target); err != nil {
		return buildFailedPublish(PublishStageJournal, err, destination), nil
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

func buildFailedPublish(stage string, err error, destination string) PublishOutput {
	return PublishOutput{Status: PublishStatusFailed, Stage: stage, Reason: err.Error(), Destination: destination}
}
