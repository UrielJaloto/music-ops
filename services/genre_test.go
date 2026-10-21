package services

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/UrielJaloto/music-ops/domain"
)

type tagSourceSpy struct {
	trackTags   map[string][]string
	artistTags  map[string][]string
	err         error
	trackCalls  int
	artistCalls int
}

func (spy *tagSourceSpy) TrackTags(_ context.Context, artist, title string) ([]string, error) {
	spy.trackCalls++
	return spy.trackTags[artist+"|"+title], spy.err
}

func (spy *tagSourceSpy) ArtistTags(_ context.Context, artist string) ([]string, error) {
	spy.artistCalls++
	return spy.artistTags[artist], spy.err
}

type genreAdvisorStub struct {
	suggestions []GenreSuggestion
	err         error
	requests    [][]GenreRequest
}

func (stub *genreAdvisorStub) SuggestGenres(_ context.Context, requests []GenreRequest) ([]GenreSuggestion, error) {
	stub.requests = append(stub.requests, requests)
	return stub.suggestions, stub.err
}

func genreTrack(t *testing.T, id, artist, title string) GenreTrack {
	t.Helper()
	titleValue, err := domain.NewTitle(title)
	if err != nil {
		t.Fatal(err)
	}
	artists, err := domain.NewArtistCredit([]string{artist})
	if err != nil {
		t.Fatal(err)
	}
	return GenreTrack{ID: domain.TrackID(id), Path: "in/" + id + ".flac", Credit: domain.NewCredit(titleValue, artists)}
}

type genreCase struct {
	name                string
	tracks              []GenreTrack
	tags                *tagSourceSpy
	advisor             *genreAdvisorStub
	expectedGenres      map[domain.TrackID]string
	expectedTrackCalls  int
	expectedArtistCalls int
	expectedWarnings    int
}

func TestGenreService_Classify(t *testing.T) {
	queen := genreTrack(t, "q1", "Queen", "Bohemian Rhapsody")
	queenOther := genreTrack(t, "q2", "Queen", "Somebody To Love")
	racionais := genreTrack(t, "r1", "Racionais MC's", "Negro Drama")
	racionaisOther := genreTrack(t, "r2", "Racionais MC's", "Vida Loka")
	cases := []genreCase{
		{
			name: "TrackTagsFound_ArtistTagsNotCalled", tracks: []GenreTrack{queen},
			tags:    &tagSourceSpy{trackTags: map[string][]string{"Queen|Bohemian Rhapsody": {"classic rock"}}},
			advisor: &genreAdvisorStub{}, expectedGenres: map[domain.TrackID]string{"q1": "Rock"},
			expectedTrackCalls: 1, expectedArtistCalls: 0, expectedWarnings: 1,
		},
		{
			name: "TrackTagsEmpty_UsesArtistTags", tracks: []GenreTrack{queen},
			tags:    &tagSourceSpy{artistTags: map[string][]string{"Queen": {"rock"}}},
			advisor: &genreAdvisorStub{}, expectedGenres: map[domain.TrackID]string{"q1": "Rock"},
			expectedTrackCalls: 1, expectedArtistCalls: 1, expectedWarnings: 1,
		},
		{
			name: "SameArtistTwice_ArtistTagsCalledOnce", tracks: []GenreTrack{queen, queenOther},
			tags:    &tagSourceSpy{artistTags: map[string][]string{"Queen": {"rock"}}},
			advisor: &genreAdvisorStub{}, expectedGenres: map[domain.TrackID]string{"q1": "Rock", "q2": "Rock"},
			expectedTrackCalls: 2, expectedArtistCalls: 1, expectedWarnings: 2,
		},
		{
			name: "TagSourceError_UsesEmptyEvidenceWithWarning", tracks: []GenreTrack{queen},
			tags: &tagSourceSpy{err: errTechnical}, advisor: &genreAdvisorStub{}, expectedGenres: map[domain.TrackID]string{"q1": "desconhecido"},
			expectedTrackCalls: 1, expectedArtistCalls: 1, expectedWarnings: 3,
		},
		{
			name: "ValidSuggestion_IsUsed", tracks: []GenreTrack{queen},
			tags:    &tagSourceSpy{},
			advisor: &genreAdvisorStub{suggestions: []GenreSuggestion{{Key: "q1", Genre: "Rock", IsKnown: true}}}, expectedGenres: map[domain.TrackID]string{"q1": "Rock"},
			expectedTrackCalls: 1, expectedArtistCalls: 1,
		},
		{
			name: "GenreOutsidePool_FallsBackToTagRules", tracks: []GenreTrack{queen},
			tags:    &tagSourceSpy{trackTags: map[string][]string{"Queen|Bohemian Rhapsody": {"pop"}}},
			advisor: &genreAdvisorStub{suggestions: []GenreSuggestion{{Key: "q1", Genre: "Polka", IsKnown: true}}}, expectedGenres: map[domain.TrackID]string{"q1": "Pop"},
			expectedTrackCalls: 1, expectedWarnings: 1,
		},
		{
			name: "AdvisorError_FallsBackToTagRules", tracks: []GenreTrack{queen},
			tags:    &tagSourceSpy{trackTags: map[string][]string{"Queen|Bohemian Rhapsody": {"rock"}}},
			advisor: &genreAdvisorStub{err: errTechnical}, expectedGenres: map[domain.TrackID]string{"q1": "Rock"},
			expectedTrackCalls: 1, expectedWarnings: 1,
		},
		{
			name: "NationalityResolvedAcrossTracks", tracks: []GenreTrack{racionais, racionaisOther},
			tags: &tagSourceSpy{trackTags: map[string][]string{"Racionais MC's|Negro Drama": {"rap"}, "Racionais MC's|Vida Loka": {"rap"}}},
			advisor: &genreAdvisorStub{suggestions: []GenreSuggestion{
				{Key: "r1", Genre: "Rap", IsBrazilian: true, IsKnown: true},
				{Key: "r2", Genre: "Rap", IsBrazilian: false, IsKnown: true},
			}},
			expectedGenres:     map[domain.TrackID]string{"r1": "Rap", "r2": "Rap"},
			expectedTrackCalls: 2,
		},
		{name: "EmptyInput_ReturnsEmptyResult", tags: &tagSourceSpy{}, advisor: &genreAdvisorStub{}, expectedGenres: map[domain.TrackID]string{}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			runGenreCase(t, testCase)
		})
	}
}

func runGenreCase(t *testing.T, testCase genreCase) {
	t.Helper()
	service := NewGenreService(testCase.tags, testCase.advisor, sequentialPool{}, 40)

	classified, err := service.Classify(context.Background(), ClassifyGenresInput{Tracks: testCase.tracks})
	if err != nil {
		t.Fatal(err)
	}
	if len(classified.Classifications) != len(testCase.expectedGenres) {
		t.Fatalf("classifications = %v", classified.Classifications)
	}
	for id, genre := range testCase.expectedGenres {
		if got := classified.Classifications[id].Genre().String(); got != genre {
			t.Fatalf("%s genre = %q, want %q", id, got, genre)
		}
	}
	if testCase.tags.trackCalls != testCase.expectedTrackCalls || testCase.tags.artistCalls != testCase.expectedArtistCalls || len(classified.Warnings) != testCase.expectedWarnings {
		t.Fatalf("track calls = %d artist calls = %d warnings = %v", testCase.tags.trackCalls, testCase.tags.artistCalls, classified.Warnings)
	}
}

func TestGenreService_RequestsCarryTagsAndArtists(t *testing.T) {
	advisor := &genreAdvisorStub{}
	tags := &tagSourceSpy{trackTags: map[string][]string{"Queen|Bohemian Rhapsody": {"Rock", "Classic Rock"}}}
	service := NewGenreService(tags, advisor, sequentialPool{}, 1)

	_, err := service.Classify(context.Background(), ClassifyGenresInput{Tracks: []GenreTrack{genreTrack(t, "q1", "Queen", "Bohemian Rhapsody")}})

	request := advisor.requests[0][0]
	if err != nil || request.Key != "q1" || request.Path != "in/q1.flac" || request.Title != "Bohemian Rhapsody" || fmt.Sprint(request.Tags) != "[rock classic rock]" || fmt.Sprint(request.Artists) != "[Queen]" {
		t.Fatalf("request = %+v err = %v", request, err)
	}
}

func TestGenreService_BatchSize(t *testing.T) {
	cases := []struct {
		size          int
		expectedError error
	}{{0, ErrBatchSizeOutOfRange}, {1, nil}, {200, nil}, {201, ErrBatchSizeOutOfRange}}
	for _, testCase := range cases {
		t.Run(fmt.Sprint(testCase.size), func(t *testing.T) {
			service := NewGenreService(&tagSourceSpy{}, &genreAdvisorStub{}, sequentialPool{}, testCase.size)

			_, err := service.Classify(context.Background(), ClassifyGenresInput{})

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v, want %v", err, testCase.expectedError)
			}
		})
	}
}

func TestGenreService_SplitsAdvisorBatches(t *testing.T) {
	advisor := &genreAdvisorStub{}
	tracks := []GenreTrack{genreTrack(t, "a", "A", "1"), genreTrack(t, "b", "B", "2"), genreTrack(t, "c", "C", "3")}

	_, err := NewGenreService(&tagSourceSpy{}, advisor, sequentialPool{}, 2).Classify(context.Background(), ClassifyGenresInput{Tracks: tracks})

	if err != nil || len(advisor.requests) != 2 || len(advisor.requests[1]) != 1 {
		t.Fatalf("batches = %d err = %v", len(advisor.requests), err)
	}
}

func TestGenreService_Cancelled_ReturnsContextError(t *testing.T) {
	service := NewGenreService(&tagSourceSpy{}, &genreAdvisorStub{}, sequentialPool{}, 1)

	_, err := service.Classify(cancelledContext(), ClassifyGenresInput{Tracks: []GenreTrack{genreTrack(t, "a", "A", "1")}})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

type failingPool struct {
	err error
}

func (pool failingPool) Run(context.Context, int, func(context.Context, int) error) error {
	return pool.err
}

type cancellingTagSource struct {
	cancel      context.CancelFunc
	cancelOnArt bool
}

func (source cancellingTagSource) TrackTags(context.Context, string, string) ([]string, error) {
	if !source.cancelOnArt {
		source.cancel()
	}
	return nil, nil
}

func (source cancellingTagSource) ArtistTags(context.Context, string) ([]string, error) {
	source.cancel()
	return nil, nil
}

func TestGenreService_PoolFailure_ReturnsWrappedError(t *testing.T) {
	service := NewGenreService(&tagSourceSpy{}, &genreAdvisorStub{}, failingPool{err: errTechnical}, 1)

	_, err := service.Classify(context.Background(), ClassifyGenresInput{Tracks: []GenreTrack{genreTrack(t, "a", "A", "1")}})

	if !errors.Is(err, errTechnical) {
		t.Fatalf("error = %v", err)
	}
}

func TestGenreService_CancelledDuringLookups(t *testing.T) {
	cases := []struct {
		name        string
		cancelOnArt bool
	}{
		{name: "DuringTrackTags_StopsArtistLookup", cancelOnArt: false},
		{name: "DuringArtistTags_StopsAdvisor", cancelOnArt: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			advisor := &genreAdvisorStub{}
			service := NewGenreService(cancellingTagSource{cancel: cancel, cancelOnArt: testCase.cancelOnArt}, advisor, sequentialPool{}, 1)

			_, err := service.Classify(ctx, ClassifyGenresInput{Tracks: []GenreTrack{genreTrack(t, "a", "A", "1")}})

			if !errors.Is(err, context.Canceled) || len(advisor.requests) != 0 {
				t.Fatalf("error = %v advisor calls = %d", err, len(advisor.requests))
			}
		})
	}
}
