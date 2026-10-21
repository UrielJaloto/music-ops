package services

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/UrielJaloto/music-ops/domain"
)

type callRecorder struct {
	calls []string
}

type creditNormalizerFake struct {
	recorder *callRecorder
	output   NormalizedCredits
	err      error
	input    NormalizeCreditsInput
}

func (fake *creditNormalizerFake) Normalize(_ context.Context, input NormalizeCreditsInput) (NormalizedCredits, error) {
	fake.recorder.calls = append(fake.recorder.calls, "credits")
	fake.input = input
	return fake.output, fake.err
}

type genreClassifierFake struct {
	recorder *callRecorder
	output   ClassifiedGenres
	err      error
	input    ClassifyGenresInput
}

func (fake *genreClassifierFake) Classify(_ context.Context, input ClassifyGenresInput) (ClassifiedGenres, error) {
	fake.recorder.calls = append(fake.recorder.calls, "genres")
	fake.input = input
	return fake.output, fake.err
}

type metadataDoubles struct {
	recorder *callRecorder
	credits  *creditNormalizerFake
	genres   *genreClassifierFake
	credit   domain.Credit
}

func metadataFixture(t *testing.T) metadataDoubles {
	t.Helper()
	recorder := &callRecorder{}
	credit := genreTrack(t, "a", "Band", "Song").Credit
	credits := &creditNormalizerFake{recorder: recorder, output: NormalizedCredits{
		Credits:  map[domain.TrackID]domain.Credit{"a": credit},
		Warnings: []string{"credit warning"},
		Failed:   []CreditFailure{{ID: "b", Path: "in/b.flac", Reason: "artist is missing"}},
	}}
	rock, err := domain.NewClassification(domain.ClassificationDetails{Genre: "Rock", IsKnown: true})
	if err != nil {
		t.Fatal(err)
	}
	genres := &genreClassifierFake{recorder: recorder, output: ClassifiedGenres{
		Classifications: map[domain.TrackID]domain.Classification{"a": rock},
		Warnings:        []string{"genre warning"},
	}}
	return metadataDoubles{recorder: recorder, credits: credits, genres: genres, credit: credit}
}

func metadataInput() ResolveMetadataInput {
	return ResolveMetadataInput{Tracks: []MetadataTrack{
		{ID: "a", Source: domain.SourceDetails{Path: "in/a.flac", Tags: domain.SourceTags{Title: "Song", Artist: "Band", AlbumArtist: "Band"}}},
		{ID: "b", Source: domain.SourceDetails{Path: "in/b.flac", Tags: domain.SourceTags{Title: "Other"}}},
	}}
}

func TestMetadataService_Resolve_ResolvesCreditsThenGenres(t *testing.T) {
	doubles := metadataFixture(t)
	recorder, credits, genres, credit := doubles.recorder, doubles.credits, doubles.genres, doubles.credit

	resolved, err := NewMetadataService(credits, genres).Resolve(context.Background(), metadataInput())

	if err != nil || !slices.Equal(recorder.calls, []string{"credits", "genres"}) {
		t.Fatalf("calls = %v err = %v", recorder.calls, err)
	}
	if credits.input.Tracks[0].Source != (domain.CreditSource{Path: "in/a.flac", Title: "Song", Artist: "Band", AlbumArtist: "Band"}) {
		t.Fatalf("credit input = %+v", credits.input.Tracks[0])
	}
	if len(genres.input.Tracks) != 1 || genres.input.Tracks[0].ID != "a" || genres.input.Tracks[0].Path != "in/a.flac" {
		t.Fatalf("genre input = %+v", genres.input.Tracks)
	}
	if len(resolved.Tracks) != 1 || resolved.Tracks[0].Credit.Artists().String() != credit.Artists().String() || resolved.Tracks[0].Classification.Genre().String() != "Rock" {
		t.Fatalf("resolved = %+v", resolved.Tracks)
	}
}

func TestMetadataService_Resolve_CreditFailure_PropagatesAsFailedTrack(t *testing.T) {
	doubles := metadataFixture(t)
	credits, genres := doubles.credits, doubles.genres

	resolved, err := NewMetadataService(credits, genres).Resolve(context.Background(), metadataInput())

	if err != nil || !slices.Equal(resolved.Failed, []MetadataFailure{{ID: "b", Path: "in/b.flac", Reason: "artist is missing"}}) {
		t.Fatalf("failed = %v err = %v", resolved.Failed, err)
	}
}

func TestMetadataService_Resolve_WarningsMerged(t *testing.T) {
	doubles := metadataFixture(t)
	credits, genres := doubles.credits, doubles.genres

	resolved, err := NewMetadataService(credits, genres).Resolve(context.Background(), metadataInput())

	if err != nil || !slices.Equal(resolved.Warnings, []string{"credit warning", "genre warning"}) {
		t.Fatalf("warnings = %v err = %v", resolved.Warnings, err)
	}
}

func TestMetadataService_Resolve_Errors(t *testing.T) {
	cases := []struct {
		name          string
		creditErr     error
		genreErr      error
		expectedCalls []string
	}{
		{name: "CreditServiceError_ReturnsWrappedError", creditErr: errTechnical, expectedCalls: []string{"credits"}},
		{name: "GenreServiceError_ReturnsWrappedError", genreErr: errTechnical, expectedCalls: []string{"credits", "genres"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			doubles := metadataFixture(t)
			recorder, credits, genres := doubles.recorder, doubles.credits, doubles.genres
			credits.err = testCase.creditErr
			genres.err = testCase.genreErr

			_, err := NewMetadataService(credits, genres).Resolve(context.Background(), metadataInput())

			if !errors.Is(err, errTechnical) || !slices.Equal(recorder.calls, testCase.expectedCalls) {
				t.Fatalf("err = %v calls = %v", err, recorder.calls)
			}
		})
	}
}

func TestMetadataService_Resolve_EmptyInput(t *testing.T) {
	recorder := &callRecorder{}
	credits := &creditNormalizerFake{recorder: recorder, output: NormalizedCredits{Credits: map[domain.TrackID]domain.Credit{}}}
	genres := &genreClassifierFake{recorder: recorder}

	resolved, err := NewMetadataService(credits, genres).Resolve(context.Background(), ResolveMetadataInput{})

	if err != nil || len(resolved.Tracks) != 0 || resolved.Tracks == nil || resolved.Failed == nil || resolved.Warnings == nil {
		t.Fatalf("resolved = %#v err = %v", resolved, err)
	}
}
