package discovery

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/UrielJaloto/music-ops/internal/domain/audio"
	"github.com/UrielJaloto/music-ops/internal/domain/tracks"
)

var (
	ErrFolderMissing        = errors.New("folder is missing")
	ErrOutputInsideInput    = errors.New("output folder must not be inside the input folder")
	ErrInspectorUnavailable = errors.New("the audio inspector is unavailable")
)

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
	Quality audio.AudioQualityDetails
	Tags    tracks.SourceTags
	Cover   tracks.CoverArt
}

type SourceFileFinder interface {
	FindAudioFiles(ctx context.Context, folder string) (FoundFiles, error)
	IsInside(parent, child string) (bool, error)
}

type AudioInspector interface {
	Inspect(ctx context.Context, path string) (InspectedAudio, error)
}

type SourceWorkPool interface {
	Run(ctx context.Context, size int, task func(ctx context.Context, index int) error) error
}

type InspectionProgress interface {
	Counted(total int)
	Finished(path string, isFailed bool)
}

type DiscoverInput struct {
	Folder   string
	Output   string
	Progress InspectionProgress
}

type DiscoveredTrack struct {
	ID     tracks.TrackID
	Source tracks.SourceDetails
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
	if err := service.validateFolders(input); err != nil {
		return DiscoveredSources{}, err
	}
	found, err := service.finder.FindAudioFiles(ctx, input.Folder)
	if err != nil {
		return DiscoveredSources{}, fmt.Errorf("finding audio files in %s: %w", input.Folder, err)
	}
	inspections := make([]sourceInspection, len(found.Audio))
	input.Progress.Counted(len(found.Audio))
	err = service.pool.Run(ctx, len(found.Audio), func(ctx context.Context, index int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		inspection, err := service.inspect(ctx, found.Audio[index])
		inspections[index] = inspection
		if err == nil {
			input.Progress.Finished(found.Audio[index].Path, inspection.failure != nil)
		}
		return err
	})
	if err != nil {
		return DiscoveredSources{}, fmt.Errorf("inspecting audio files: %w", err)
	}
	return collectDiscovered(inspections, found.Ignored), nil
}

func (service SourceService) validateFolders(input DiscoverInput) error {
	if strings.TrimSpace(input.Folder) == "" || strings.TrimSpace(input.Output) == "" {
		return ErrFolderMissing
	}
	isInside, err := service.finder.IsInside(input.Folder, input.Output)
	if err != nil {
		return fmt.Errorf("comparing %s with %s: %w", input.Output, input.Folder, err)
	}
	if isInside {
		return ErrOutputInsideInput
	}
	return nil
}

type sourceInspection struct {
	track   DiscoveredTrack
	failure *SourceFailure
}

func (service SourceService) inspect(ctx context.Context, file FoundFile) (sourceInspection, error) {
	inspected, err := service.inspector.Inspect(ctx, file.Path)
	if errors.Is(err, ErrInspectorUnavailable) {
		return sourceInspection{}, err
	}
	if err != nil {
		return buildFailedInspection(file, err), nil
	}
	source, err := toSourceDetails(file, inspected)
	if err != nil {
		return buildFailedInspection(file, err), nil
	}
	id := tracks.NewTrackID(file.Path, file.Size, file.ModifiedAt)
	return sourceInspection{track: DiscoveredTrack{ID: id, Source: source}}, nil
}

func toSourceDetails(file FoundFile, inspected InspectedAudio) (tracks.SourceDetails, error) {
	codec, err := audio.NewCodec(inspected.Codec)
	if err != nil {
		return tracks.SourceDetails{}, err
	}
	quality, err := audio.NewAudioQuality(inspected.Quality)
	if err != nil {
		return tracks.SourceDetails{}, err
	}
	return tracks.SourceDetails{
		Path:         file.Path,
		RelativePath: file.RelativePath,
		Directory:    file.Directory,
		Codec:        codec,
		Quality:      quality,
		Tags:         inspected.Tags,
		Cover:        inspected.Cover,
	}, nil
}

func buildFailedInspection(file FoundFile, err error) sourceInspection {
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
