package metadata

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/UrielJaloto/music-ops/internal/domain/credits"
	"github.com/UrielJaloto/music-ops/internal/domain/genres"
	"github.com/UrielJaloto/music-ops/internal/domain/tracks"
	"github.com/UrielJaloto/music-ops/internal/services/classification"
	"github.com/UrielJaloto/music-ops/internal/services/crediting"
)

type callRecorder struct {
	calls []string
}

type creditNormalizerFake struct {
	recorder *callRecorder
	output   crediting.NormalizedCredits
	err      error
	input    crediting.NormalizeCreditsInput
}

func (fake *creditNormalizerFake) Normalize(_ context.Context, input crediting.NormalizeCreditsInput) (crediting.NormalizedCredits, error) {
	fake.recorder.calls = append(fake.recorder.calls, "credits")
	fake.input = input
	return fake.output, fake.err
}

type genreClassifierFake struct {
	recorder *callRecorder
	output   classification.ClassifiedGenres
	err      error
	input    classification.ClassifyGenresInput
}

func (fake *genreClassifierFake) Classify(_ context.Context, input classification.ClassifyGenresInput) (classification.ClassifiedGenres, error) {
	fake.recorder.calls = append(fake.recorder.calls, "genres")
	fake.input = input
	return fake.output, fake.err
}

type metadataDoubles struct {
	recorder *callRecorder
	credits  *creditNormalizerFake
	genres   *genreClassifierFake
	credit   credits.Credit
}

func metadataFixture(t *testing.T) metadataDoubles {
	t.Helper()
	recorder := &callRecorder{}
	credit := genreTrack(t, "a", "Band", "Song").Credit
	credits := &creditNormalizerFake{recorder: recorder, output: crediting.NormalizedCredits{
		Credits:  map[tracks.TrackID]credits.Credit{"a": credit},
		Warnings: []string{"credit warning"},
		Failed:   []crediting.CreditFailure{{ID: "b", Path: "in/b.flac", Reason: "artist is missing"}},
	}}
	rock, err := genres.NewClassification(genres.ClassificationDetails{Genre: "Rock", IsKnown: true})
	if err != nil {
		t.Fatal(err)
	}
	genres := &genreClassifierFake{recorder: recorder, output: classification.ClassifiedGenres{
		Classifications: map[tracks.TrackID]genres.Classification{"a": rock},
		Warnings:        []string{"genre warning"},
	}}
	return metadataDoubles{recorder: recorder, credits: credits, genres: genres, credit: credit}
}

func metadataInput() ResolveMetadataInput {
	return ResolveMetadataInput{Tracks: []MetadataTrack{
		{ID: "a", Source: tracks.SourceDetails{Path: "in/a.flac", Tags: tracks.SourceTags{Title: "Song", Artist: "Band", AlbumArtist: "Band"}}},
		{ID: "b", Source: tracks.SourceDetails{Path: "in/b.flac", Tags: tracks.SourceTags{Title: "Other"}}},
	}}
}

func TestMetadataService_Resolve_ResolvesCreditsThenGenres(t *testing.T) {
	doubles := metadataFixture(t)
	recorder, creditFake, genreFake, credit := doubles.recorder, doubles.credits, doubles.genres, doubles.credit

	resolved, err := NewMetadataService(creditFake, genreFake).Resolve(context.Background(), metadataInput())

	if err != nil || !slices.Equal(recorder.calls, []string{"credits", "genres"}) {
		t.Fatalf("calls = %v err = %v", recorder.calls, err)
	}
	if creditFake.input.Tracks[0].Source != (credits.CreditSource{Path: "in/a.flac", Title: "Song", Artist: "Band", AlbumArtist: "Band"}) {
		t.Fatalf("credit input = %+v", creditFake.input.Tracks[0])
	}
	if len(genreFake.input.Tracks) != 1 || genreFake.input.Tracks[0].ID != "a" || genreFake.input.Tracks[0].Path != "in/a.flac" {
		t.Fatalf("genre input = %+v", genreFake.input.Tracks)
	}
	if len(resolved.Tracks) != 1 || resolved.Tracks[0].Credit.Artists().String() != credit.Artists().String() || resolved.Tracks[0].Classification.Genre().String() != "Rock" {
		t.Fatalf("resolved = %+v", resolved.Tracks)
	}
}

func TestMetadataService_Resolve_UntaggedTrack_TitleFromFileName(t *testing.T) {
	doubles := metadataFixture(t)
	input := ResolveMetadataInput{Tracks: []MetadataTrack{{ID: "a", Source: tracks.SourceDetails{Path: "Band/Song - Band.m4a"}}}}

	_, err := NewMetadataService(doubles.credits, doubles.genres).Resolve(context.Background(), input)

	if err != nil || doubles.credits.input.Tracks[0].Source != (credits.CreditSource{Path: "Band/Song - Band.m4a", Title: "Song", Artist: "Band"}) {
		t.Fatalf("credit input = %+v err = %v", doubles.credits.input.Tracks, err)
	}
}

func TestMetadataService_Resolve_FixedGenre_SkipsClassifier(t *testing.T) {
	doubles := metadataFixture(t)
	input := metadataInput()
	input.FixedGenre = "Rock; Metal"

	resolved, err := NewMetadataService(doubles.credits, doubles.genres).Resolve(context.Background(), input)

	if err != nil || !slices.Equal(doubles.recorder.calls, []string{"credits"}) {
		t.Fatalf("calls = %v err = %v", doubles.recorder.calls, err)
	}
	if len(resolved.Tracks) != 1 || resolved.Tracks[0].Classification.Genre().String() != "Rock; Metal" || slices.Contains(resolved.Warnings, "genre warning") {
		t.Fatalf("resolved = %+v", resolved)
	}
}

func TestMetadataService_Resolve_FixedGenreNotInPool_ReturnsError(t *testing.T) {
	doubles := metadataFixture(t)
	input := metadataInput()
	input.FixedGenre = "Nu Metal"

	_, err := NewMetadataService(doubles.credits, doubles.genres).Resolve(context.Background(), input)

	if !errors.Is(err, genres.ErrGenreNotInPool) || slices.Contains(doubles.recorder.calls, "genres") {
		t.Fatalf("calls = %v err = %v", doubles.recorder.calls, err)
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
	credits := &creditNormalizerFake{recorder: recorder, output: crediting.NormalizedCredits{Credits: map[tracks.TrackID]credits.Credit{}}}
	genres := &genreClassifierFake{recorder: recorder}

	resolved, err := NewMetadataService(credits, genres).Resolve(context.Background(), ResolveMetadataInput{})

	if err != nil || len(resolved.Tracks) != 0 || resolved.Tracks == nil || resolved.Failed == nil || resolved.Warnings == nil {
		t.Fatalf("resolved = %#v err = %v", resolved, err)
	}
}

var errTechnical = errors.New("technical failure")

func genreTrack(t *testing.T, id, artist, title string) classification.GenreTrack {
	t.Helper()
	titleValue, err := credits.NewTitle(title)
	if err != nil {
		t.Fatal(err)
	}
	artists, err := credits.NewArtistCredit([]string{artist})
	if err != nil {
		t.Fatal(err)
	}
	return classification.GenreTrack{ID: tracks.TrackID(id), Path: "in/" + id + ".flac", Credit: credits.NewCredit(titleValue, artists)}
}
