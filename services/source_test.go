package services

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/domain"
)

type sequentialPool struct{}

func (sequentialPool) Run(ctx context.Context, size int, task func(ctx context.Context, index int) error) error {
	for index := range size {
		if err := task(ctx, index); err != nil {
			return err
		}
	}
	return nil
}

type finderStub struct {
	found FoundFiles
	err   error
}

func (stub finderStub) FindAudioFiles(context.Context, string) (FoundFiles, error) {
	return stub.found, stub.err
}

type inspectorStub struct {
	results map[string]InspectedAudio
	errors  map[string]error
}

func (stub inspectorStub) Inspect(_ context.Context, path string) (InspectedAudio, error) {
	return stub.results[path], stub.errors[path]
}

var errTechnical = errors.New("technical failure")

func validInspection() InspectedAudio {
	return InspectedAudio{
		Codec:   "FLAC",
		Quality: domain.AudioQualityDetails{SampleRate: 44100, BitDepth: 16, Channels: 2, Duration: time.Minute},
		Tags:    domain.SourceTags{Title: "Song", Artist: "Band"},
		Cover:   domain.CoverArt{Stream: domain.NoCoverStream},
	}
}

func foundFile(relative string) FoundFile {
	return FoundFile{Path: "in/" + relative, RelativePath: relative, Directory: "in", Size: 10, ModifiedAt: time.Unix(1, 0)}
}

func TestSourceService_Discover(t *testing.T) {
	invalidQuality := validInspection()
	invalidQuality.Quality.SampleRate = 0
	files := FoundFiles{Audio: []FoundFile{foundFile("b.flac"), foundFile("a.flac")}, Ignored: []string{"in/cover.jpg"}}
	cases := []struct {
		name           string
		input          DiscoverInput
		finder         finderStub
		inspector      inspectorStub
		ctx            func() context.Context
		expectedPaths  []string
		expectedFailed []SourceFailure
		expectedError  error
	}{
		{
			name: "WithAudioFiles_ReturnsTracksInPathOrder", input: DiscoverInput{Folder: "in"}, finder: finderStub{found: files},
			inspector:     inspectorStub{results: map[string]InspectedAudio{"in/a.flac": validInspection(), "in/b.flac": validInspection()}},
			expectedPaths: []string{"a.flac", "b.flac"}, expectedFailed: []SourceFailure{},
		},
		{name: "WithEmptyFolder_ReturnsErrFolderMissing", input: DiscoverInput{Folder: " "}, expectedError: ErrFolderMissing},
		{name: "WhenFinderFails_ReturnsWrappedError", input: DiscoverInput{Folder: "in"}, finder: finderStub{err: errTechnical}, expectedError: errTechnical},
		{
			name: "WhenInspectionFails_RecordsFailureAndContinues", input: DiscoverInput{Folder: "in"}, finder: finderStub{found: files},
			inspector: inspectorStub{
				results: map[string]InspectedAudio{"in/a.flac": validInspection()},
				errors:  map[string]error{"in/b.flac": errTechnical},
			},
			expectedPaths: []string{"a.flac"}, expectedFailed: []SourceFailure{{Path: "in/b.flac", Reason: "technical failure"}},
		},
		{
			name: "WhenQualityInvalid_RecordsFailure", input: DiscoverInput{Folder: "in"}, finder: finderStub{found: FoundFiles{Audio: []FoundFile{foundFile("a.flac")}}},
			inspector:     inspectorStub{results: map[string]InspectedAudio{"in/a.flac": invalidQuality}},
			expectedPaths: []string{}, expectedFailed: []SourceFailure{{Path: "in/a.flac", Reason: domain.ErrSampleRateOutOfRange.Error()}},
		},
		{
			name: "WhenCodecMissing_RecordsFailure", input: DiscoverInput{Folder: "in"}, finder: finderStub{found: FoundFiles{Audio: []FoundFile{foundFile("a.flac")}}},
			inspector:     inspectorStub{results: map[string]InspectedAudio{"in/a.flac": {}}},
			expectedPaths: []string{}, expectedFailed: []SourceFailure{{Path: "in/a.flac", Reason: domain.ErrCodecMissing.Error()}},
		},
		{
			name: "CancelledContext_StopsAndReturnsContextError", input: DiscoverInput{Folder: "in"}, finder: finderStub{found: files},
			ctx:           cancelledContext,
			expectedError: context.Canceled,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := NewSourceService(testCase.finder, testCase.inspector, sequentialPool{})
			ctx := context.Background()
			if testCase.ctx != nil {
				ctx = testCase.ctx()
			}

			discovered, err := service.Discover(ctx, testCase.input)

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v, want %v", err, testCase.expectedError)
			}
			if err != nil {
				return
			}
			assertDiscovered(t, discovered, testCase.expectedPaths, testCase.expectedFailed)
		})
	}
}

func assertDiscovered(t *testing.T, discovered DiscoveredSources, expectedPaths []string, expectedFailed []SourceFailure) {
	t.Helper()
	paths := make([]string, 0, len(discovered.Tracks))
	for _, track := range discovered.Tracks {
		paths = append(paths, track.Source.RelativePath)
	}
	if !slices.Equal(paths, expectedPaths) || !slices.Equal(discovered.Failed, expectedFailed) {
		t.Fatalf("paths = %v failed = %v", paths, discovered.Failed)
	}
}

func TestSourceService_Discover_MapsSourceDetails(t *testing.T) {
	file := foundFile("a.flac")
	service := NewSourceService(finderStub{found: FoundFiles{Audio: []FoundFile{file}, Ignored: []string{"in/cover.jpg"}}},
		inspectorStub{results: map[string]InspectedAudio{"in/a.flac": validInspection()}}, sequentialPool{})

	discovered, err := service.Discover(context.Background(), DiscoverInput{Folder: "in"})
	if err != nil {
		t.Fatal(err)
	}
	track := discovered.Tracks[0]
	if track.ID != domain.NewTrackID(file.Path, file.Size, file.ModifiedAt) || track.Source.Codec != "flac" || track.Source.Tags.Title != "Song" {
		t.Fatalf("track = %+v", track)
	}
	if track.Source.Quality.SampleRate() != 44100 || track.Source.Cover.Stream != domain.NoCoverStream || track.Source.Directory != "in" {
		t.Fatalf("source = %+v", track.Source)
	}
	if !slices.Equal(discovered.Ignored, []string{"in/cover.jpg"}) {
		t.Fatalf("IgnoredFiles_ArePassedThrough: %v", discovered.Ignored)
	}
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
