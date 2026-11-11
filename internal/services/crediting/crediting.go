package crediting

import (
	"context"
	"errors"
	"fmt"

	"github.com/UrielJaloto/music-ops/internal/domain/credits"
	"github.com/UrielJaloto/music-ops/internal/domain/tracks"
)

var ErrBatchSizeOutOfRange = errors.New("advisor batch size must be between 1 and 200")

const (
	minimumBatchSize = 1
	maximumBatchSize = 200
)

var (
	ErrSuggestionMissing      = errors.New("advisor gave no suggestion")
	errSuggestionInconsistent = errors.New("advisor suggestion is not traceable to the source")
)

type CreditRequest struct {
	Key    string
	Source credits.CreditSource
}

type CreditSuggestion struct {
	Key     string
	Title   string
	Artists []string
}

type CreditAdvisor interface {
	SuggestCredits(ctx context.Context, requests []CreditRequest) ([]CreditSuggestion, error)
}

type CreditTrack struct {
	ID     tracks.TrackID
	Source credits.CreditSource
}

type NormalizeCreditsInput struct {
	Tracks []CreditTrack
}

type CreditFailure struct {
	ID     tracks.TrackID
	Path   string
	Reason string
}

type NormalizedCredits struct {
	Credits  map[tracks.TrackID]credits.Credit
	Warnings []string
	Failed   []CreditFailure
}

type CreditService struct {
	advisor   CreditAdvisor
	batchSize int
}

func NewCreditService(advisor CreditAdvisor, batchSize int) CreditService {
	return CreditService{advisor: advisor, batchSize: batchSize}
}

func (service CreditService) Normalize(ctx context.Context, input NormalizeCreditsInput) (NormalizedCredits, error) {
	if !IsValidBatchSize(service.batchSize) {
		return NormalizedCredits{}, ErrBatchSizeOutOfRange
	}
	normalized := NormalizedCredits{Credits: map[tracks.TrackID]credits.Credit{}, Warnings: []string{}, Failed: []CreditFailure{}}
	for start := 0; start < len(input.Tracks); start += service.batchSize {
		if err := ctx.Err(); err != nil {
			return NormalizedCredits{}, err
		}
		batch := input.Tracks[start:min(start+service.batchSize, len(input.Tracks))]
		normalized = service.normalizeBatch(ctx, batch, normalized)
	}
	return normalized, nil
}

func (service CreditService) normalizeBatch(ctx context.Context, batch []CreditTrack, normalized NormalizedCredits) NormalizedCredits {
	suggestions, err := service.advisor.SuggestCredits(ctx, toCreditRequests(batch))
	for _, track := range batch {
		normalized = applyCredit(normalized, track, chooseCredit(track, suggestions, err))
	}
	return normalized
}

func IsValidBatchSize(size int) bool {
	return size >= minimumBatchSize && size <= maximumBatchSize
}

func toCreditRequests(tracks []CreditTrack) []CreditRequest {
	requests := make([]CreditRequest, 0, len(tracks))
	for _, track := range tracks {
		requests = append(requests, CreditRequest{Key: string(track.ID), Source: track.Source})
	}
	return requests
}

type creditChoice struct {
	credit       credits.Credit
	fallbackWhy  error
	isSuggestion bool
}

func chooseCredit(track CreditTrack, suggestions []CreditSuggestion, batchErr error) creditChoice {
	if batchErr != nil {
		return creditChoice{fallbackWhy: batchErr}
	}
	credit, err := buildSuggestedCredit(track, suggestions)
	if err != nil {
		return creditChoice{fallbackWhy: err}
	}
	return creditChoice{credit: credit, isSuggestion: true}
}

func buildSuggestedCredit(track CreditTrack, suggestions []CreditSuggestion) (credits.Credit, error) {
	suggestion, found := findCreditSuggestion(suggestions, string(track.ID))
	if !found {
		return credits.Credit{}, ErrSuggestionMissing
	}
	title, err := credits.NewTitle(suggestion.Title)
	if err != nil {
		return credits.Credit{}, err
	}
	artists, err := credits.NewArtistCredit(suggestion.Artists)
	if err != nil {
		return credits.Credit{}, err
	}
	credit := credits.NewCredit(title, artists)
	if !credit.IsConsistentWith(track.Source) {
		return credits.Credit{}, errSuggestionInconsistent
	}
	return credit, nil
}

func findCreditSuggestion(suggestions []CreditSuggestion, key string) (CreditSuggestion, bool) {
	for _, suggestion := range suggestions {
		if suggestion.Key == key {
			return suggestion, true
		}
	}
	return CreditSuggestion{}, false
}

func applyCredit(normalized NormalizedCredits, track CreditTrack, choice creditChoice) NormalizedCredits {
	if choice.isSuggestion {
		normalized.Credits[track.ID] = choice.credit
		return normalized
	}
	normalized.Warnings = append(normalized.Warnings, fmt.Sprintf("credit rules used for %s: %v", track.Source.Path, choice.fallbackWhy))
	credit, err := track.Source.BuildCreditFromRules()
	if err != nil {
		normalized.Failed = append(normalized.Failed, CreditFailure{ID: track.ID, Path: track.Source.Path, Reason: err.Error()})
		return normalized
	}
	normalized.Credits[track.ID] = credit
	return normalized
}
