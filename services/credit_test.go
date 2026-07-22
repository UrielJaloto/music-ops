package services

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/UrielJaloto/music-ops/domain"
)

type creditAdvisorSpy struct {
	suggestions map[string]CreditSuggestion
	err         error
	batches     [][]CreditRequest
}

func (spy *creditAdvisorSpy) SuggestCredits(_ context.Context, requests []CreditRequest) ([]CreditSuggestion, error) {
	spy.batches = append(spy.batches, requests)
	if spy.err != nil {
		return nil, spy.err
	}
	suggestions := []CreditSuggestion{}
	for _, request := range requests {
		if suggestion, found := spy.suggestions[request.Key]; found {
			suggestions = append(suggestions, suggestion)
		}
	}
	return suggestions, nil
}

func calabriaTrack() CreditTrack {
	return CreditTrack{ID: "t1", Source: domain.CreditSource{
		Path: "in/Alex Gaudino/01.flac", Title: "Destination Calabria (feat. Crystal Waters) [Radio Edit]", Artist: "Alex Gaudino",
	}}
}

func TestCreditService_Normalize(t *testing.T) {
	cases := []struct {
		name            string
		suggestions     map[string]CreditSuggestion
		advisorErr      error
		expectedArtists string
		expectedTitle   string
		expectedWarning bool
	}{
		{
			name:            "ValidSuggestion_IsAccepted",
			suggestions:     map[string]CreditSuggestion{"t1": {Key: "t1", Title: "Destination Calabria [Radio Edit]", Artists: []string{"Alex Gaudino", "Crystal Waters"}}},
			expectedArtists: "Alex Gaudino; Crystal Waters", expectedTitle: "Destination Calabria [Radio Edit]",
		},
		{
			name:            "InventedArtist_FallsBackToRules",
			suggestions:     map[string]CreditSuggestion{"t1": {Key: "t1", Title: "Destination Calabria [Radio Edit]", Artists: []string{"Alex Gaudino", "Madonna"}}},
			expectedArtists: "Alex Gaudino; Crystal Waters", expectedTitle: "Destination Calabria [Radio Edit]", expectedWarning: true,
		},
		{
			name:            "ChangedTitle_FallsBackToRules",
			suggestions:     map[string]CreditSuggestion{"t1": {Key: "t1", Title: "Destination Calabria", Artists: []string{"Alex Gaudino"}}},
			expectedArtists: "Alex Gaudino; Crystal Waters", expectedTitle: "Destination Calabria [Radio Edit]", expectedWarning: true,
		},
		{
			name:            "EmptyTitleSuggestion_FallsBackToRules",
			suggestions:     map[string]CreditSuggestion{"t1": {Key: "t1", Title: " ", Artists: []string{"Alex Gaudino"}}},
			expectedArtists: "Alex Gaudino; Crystal Waters", expectedTitle: "Destination Calabria [Radio Edit]", expectedWarning: true,
		},
		{
			name:            "EmptyArtistsSuggestion_FallsBackToRules",
			suggestions:     map[string]CreditSuggestion{"t1": {Key: "t1", Title: "Destination Calabria [Radio Edit]"}},
			expectedArtists: "Alex Gaudino; Crystal Waters", expectedTitle: "Destination Calabria [Radio Edit]", expectedWarning: true,
		},
		{
			name:            "MissingSuggestion_FallsBackToRules",
			suggestions:     map[string]CreditSuggestion{},
			expectedArtists: "Alex Gaudino; Crystal Waters", expectedTitle: "Destination Calabria [Radio Edit]", expectedWarning: true,
		},
		{
			name:            "AdvisorError_FallsBackForWholeBatch",
			advisorErr:      errTechnical,
			expectedArtists: "Alex Gaudino; Crystal Waters", expectedTitle: "Destination Calabria [Radio Edit]", expectedWarning: true,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			advisor := &creditAdvisorSpy{suggestions: testCase.suggestions, err: testCase.advisorErr}
			service := NewCreditService(advisor, 40)

			normalized, err := service.Normalize(context.Background(), NormalizeCreditsInput{Tracks: []CreditTrack{calabriaTrack()}})
			if err != nil {
				t.Fatal(err)
			}
			credit := normalized.Credits["t1"]
			if credit.Artists().String() != testCase.expectedArtists || credit.Title().String() != testCase.expectedTitle {
				t.Fatalf("credit = %q / %q", credit.Title().String(), credit.Artists().String())
			}
			if (len(normalized.Warnings) == 1) != testCase.expectedWarning || len(normalized.Failed) != 0 {
				t.Fatalf("warnings = %v failed = %v", normalized.Warnings, normalized.Failed)
			}
		})
	}
}

func TestCreditService_Normalize_WarningText(t *testing.T) {
	service := NewCreditService(&creditAdvisorSpy{err: errTechnical}, 1)

	normalized, err := service.Normalize(context.Background(), NormalizeCreditsInput{Tracks: []CreditTrack{calabriaTrack()}})

	if err != nil || !slices.Equal(normalized.Warnings, []string{"credit rules used for in/Alex Gaudino/01.flac: technical failure"}) {
		t.Fatalf("warnings = %v err = %v", normalized.Warnings, err)
	}
}

func TestCreditService_BatchSizeBoundaries(t *testing.T) {
	cases := []struct {
		size          int
		expectedError error
	}{{0, ErrBatchSizeOutOfRange}, {1, nil}, {200, nil}, {201, ErrBatchSizeOutOfRange}, {-1, ErrBatchSizeOutOfRange}}
	for _, testCase := range cases {
		t.Run(fmt.Sprint(testCase.size), func(t *testing.T) {
			service := NewCreditService(&creditAdvisorSpy{}, testCase.size)

			_, err := service.Normalize(context.Background(), NormalizeCreditsInput{})

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v, want %v", err, testCase.expectedError)
			}
		})
	}
}

func TestCreditService_SplitsIntoBatches(t *testing.T) {
	const size = 3
	cases := []struct {
		tracks          int
		expectedBatches int
	}{{0, 0}, {1, 1}, {size, 1}, {size + 1, 2}}
	for _, testCase := range cases {
		t.Run(fmt.Sprint(testCase.tracks), func(t *testing.T) {
			advisor := &creditAdvisorSpy{}
			tracks := make([]CreditTrack, 0, testCase.tracks)
			for index := range testCase.tracks {
				track := calabriaTrack()
				track.ID = domain.TrackID(fmt.Sprint(index))
				tracks = append(tracks, track)
			}

			normalized, err := NewCreditService(advisor, size).Normalize(context.Background(), NormalizeCreditsInput{Tracks: tracks})

			if err != nil || len(advisor.batches) != testCase.expectedBatches || len(normalized.Credits) != testCase.tracks {
				t.Fatalf("batches = %d credits = %d err = %v", len(advisor.batches), len(normalized.Credits), err)
			}
		})
	}
}

func TestCreditService_RulesFail_RecordsFailure(t *testing.T) {
	track := CreditTrack{ID: "t1", Source: domain.CreditSource{Path: "in/song.flac", Title: "Song"}}

	normalized, err := NewCreditService(&creditAdvisorSpy{}, 1).Normalize(context.Background(), NormalizeCreditsInput{Tracks: []CreditTrack{track}})

	expected := []CreditFailure{{ID: "t1", Path: "in/song.flac", Reason: domain.ErrArtistMissing.Error()}}
	if err != nil || !slices.Equal(normalized.Failed, expected) || len(normalized.Credits) != 0 {
		t.Fatalf("normalized = %+v err = %v", normalized, err)
	}
}

func TestCreditService_Cancelled_ReturnsContextError(t *testing.T) {
	_, err := NewCreditService(&creditAdvisorSpy{}, 1).Normalize(cancelledContext(), NormalizeCreditsInput{Tracks: []CreditTrack{calabriaTrack()}})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}
