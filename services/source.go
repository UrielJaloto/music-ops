package services

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/UrielJaloto/music-ops/domain"
)

var ErrFolderMissing = errors.New("folder is missing")

type FoundFile struct {
	Path         string
	RelativePath string
	Directory    string
	Size         int64
	ModifiedAt   time.Time
}

type FoundFiles struct {
	Audio   []FoundFile
	Ignored []string
}

type InspectedAudio struct {
	Codec   string
	Quality domain.AudioQualityDetails
	Tags    domain.SourceTags
	Cover   domain.CoverArt
}

type SourceFileFinder interface {
	FindAudioFiles(ctx context.Context, folder string) (FoundFiles, error)
}

type AudioInspector interface {
	Inspect(ctx context.Context, path string) (InspectedAudio, error)
}

type SourceWorkPool interface {
	Run(ctx context.Context, size int, task func(ctx context.Context, index int) error) error
}

type DiscoverInput struct {
	Folder string
}

type DiscoveredTrack struct {
	ID     domain.TrackID
	Source domain.SourceDetails
}

type SourceFailure struct {
	Path   string
	Reason string
}

type DiscoveredSources struct {
	Tracks  []DiscoveredTrack
	Ignored []string
	Failed  []SourceFailure
}

type SourceService struct {
	finder    SourceFileFinder
	inspector AudioInspector
	pool      SourceWorkPool
}

func NewSourceService(finder SourceFileFinder, inspector AudioInspector, pool SourceWorkPool) SourceService {
	return SourceService{finder: finder, inspector: inspector, pool: pool}
}

func (service SourceService) Discover(ctx context.Context, input DiscoverInput) (DiscoveredSources, error) {
	if strings.TrimSpace(input.Folder) == "" {
		return DiscoveredSources{}, ErrFolderMissing
	}
	found, err := service.finder.FindAudioFiles(ctx, input.Folder)
	if err != nil {
		return DiscoveredSources{}, fmt.Errorf("finding audio files in %s: %w", input.Folder, err)
	}
	inspections := make([]sourceInspection, len(found.Audio))
	err = service.pool.Run(ctx, len(found.Audio), func(ctx context.Context, index int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		inspections[index] = service.inspect(ctx, found.Audio[index])
		return nil
	})
	if err != nil {
		return DiscoveredSources{}, fmt.Errorf("inspecting audio files: %w", err)
	}
	return collectDiscovered(inspections, found.Ignored), nil
}

type sourceInspection struct {
	track   DiscoveredTrack
	failure *SourceFailure
}

func (service SourceService) inspect(ctx context.Context, file FoundFile) sourceInspection {
	inspected, err := service.inspector.Inspect(ctx, file.Path)
	if err != nil {
		return failedInspection(file, err)
	}
	source, err := toSourceDetails(file, inspected)
	if err != nil {
		return failedInspection(file, err)
	}
	id := domain.NewTrackID(file.Path, file.Size, file.ModifiedAt)
	return sourceInspection{track: DiscoveredTrack{ID: id, Source: source}}
}

func toSourceDetails(file FoundFile, inspected InspectedAudio) (domain.SourceDetails, error) {
	codec, err := domain.NewCodec(inspected.Codec)
	if err != nil {
		return domain.SourceDetails{}, err
	}
	quality, err := domain.NewAudioQuality(inspected.Quality)
	if err != nil {
		return domain.SourceDetails{}, err
	}
	return domain.SourceDetails{
		Path:         file.Path,
		RelativePath: file.RelativePath,
		Directory:    file.Directory,
		Codec:        codec,
		Quality:      quality,
		Tags:         inspected.Tags,
		Cover:        inspected.Cover,
	}, nil
}

func failedInspection(file FoundFile, err error) sourceInspection {
	return sourceInspection{failure: &SourceFailure{Path: file.Path, Reason: err.Error()}}
}

func collectDiscovered(inspections []sourceInspection, ignored []string) DiscoveredSources {
	discovered := DiscoveredSources{Tracks: []DiscoveredTrack{}, Ignored: append([]string{}, ignored...), Failed: []SourceFailure{}}
	for _, inspection := range inspections {
		if inspection.failure != nil {
			discovered.Failed = append(discovered.Failed, *inspection.failure)
			continue
		}
		discovered.Tracks = append(discovered.Tracks, inspection.track)
	}
	slices.SortStableFunc(discovered.Tracks, func(left, right DiscoveredTrack) int {
		return strings.Compare(left.Source.RelativePath, right.Source.RelativePath)
	})
	return discovered
}
