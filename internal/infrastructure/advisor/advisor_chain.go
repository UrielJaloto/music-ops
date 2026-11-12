package advisor

import (
	"context"
	"strings"

	"github.com/UrielJaloto/music-ops/internal/domain/credits"
	"github.com/UrielJaloto/music-ops/internal/domain/genres"
	"github.com/UrielJaloto/music-ops/internal/services/classification"
	"github.com/UrielJaloto/music-ops/internal/services/crediting"
	"github.com/UrielJaloto/music-ops/internal/services/diagnostics"
)

const advisorNameSeparator = ","

type ChainedAdvisor interface {
	SuggestCredits(ctx context.Context, requests []crediting.CreditRequest) ([]crediting.CreditSuggestion, error)
	SuggestGenres(ctx context.Context, requests []classification.GenreRequest) ([]classification.GenreSuggestion, error)
	Name() string
	ProbeAdvisor(ctx context.Context) diagnostics.AdvisorReport
}

type CreditValidator func(request crediting.CreditRequest, suggestion crediting.CreditSuggestion) bool

type GenreValidator func(request classification.GenreRequest, suggestion classification.GenreSuggestion) bool

type AdvisorChain struct {
	advisors    []ChainedAdvisor
	validCredit CreditValidator
	validGenre  GenreValidator
}

func NewAdvisorChain(advisors []ChainedAdvisor, validCredit CreditValidator, validGenre GenreValidator) AdvisorChain {
	return AdvisorChain{advisors: advisors, validCredit: validCredit, validGenre: validGenre}
}

func (chain AdvisorChain) SuggestCredits(ctx context.Context, requests []crediting.CreditRequest) ([]crediting.CreditSuggestion, error) {
	steps := make([]chainStep[crediting.CreditRequest, crediting.CreditSuggestion], 0, len(chain.advisors))
	for _, advisor := range chain.advisors {
		steps = append(steps, advisor.SuggestCredits)
	}
	link := chainLink[crediting.CreditRequest, crediting.CreditSuggestion]{
		requestKey: func(request crediting.CreditRequest) string { return request.Key },
		answerKey:  func(suggestion crediting.CreditSuggestion) string { return suggestion.Key },
		isValid:    chain.validCredit,
	}
	return runChain(ctx, steps, link, requests), nil
}

func (chain AdvisorChain) SuggestGenres(ctx context.Context, requests []classification.GenreRequest) ([]classification.GenreSuggestion, error) {
	steps := make([]chainStep[classification.GenreRequest, classification.GenreSuggestion], 0, len(chain.advisors))
	for _, advisor := range chain.advisors {
		steps = append(steps, advisor.SuggestGenres)
	}
	link := chainLink[classification.GenreRequest, classification.GenreSuggestion]{
		requestKey: func(request classification.GenreRequest) string { return request.Key },
		answerKey:  func(suggestion classification.GenreSuggestion) string { return suggestion.Key },
		isValid:    chain.validGenre,
	}
	return runChain(ctx, steps, link, requests), nil
}

func (chain AdvisorChain) ProbeAdvisors(ctx context.Context) []diagnostics.AdvisorReport {
	reports := make([]diagnostics.AdvisorReport, 0, len(chain.advisors))
	for _, advisor := range chain.advisors {
		reports = append(reports, advisor.ProbeAdvisor(ctx))
	}
	return reports
}

func (chain AdvisorChain) Names() string {
	names := make([]string, 0, len(chain.advisors))
	for _, advisor := range chain.advisors {
		names = append(names, advisor.Name())
	}
	return strings.Join(names, advisorNameSeparator)
}

type chainStep[R, S any] func(ctx context.Context, requests []R) ([]S, error)

type chainLink[R, S any] struct {
	requestKey func(R) string
	answerKey  func(S) string
	isValid    func(R, S) bool
}

func runChain[R, S any](ctx context.Context, steps []chainStep[R, S], link chainLink[R, S], requests []R) []S {
	accepted := []S{}
	pending := requests
	for _, step := range steps {
		if len(pending) == 0 || ctx.Err() != nil {
			break
		}
		answers, err := step(ctx, pending)
		if err != nil {
			continue
		}
		var taken []S
		taken, pending = acceptAnswers(link, pending, answers)
		accepted = append(accepted, taken...)
	}
	return accepted
}

func acceptAnswers[R, S any](link chainLink[R, S], pending []R, answers []S) ([]S, []R) {
	byKey := map[string]S{}
	for _, answer := range answers {
		byKey[link.answerKey(answer)] = answer
	}
	accepted := []S{}
	remaining := []R{}
	for _, request := range pending {
		answer, isAnswered := byKey[link.requestKey(request)]
		if !isAnswered || !link.isValid(request, answer) {
			remaining = append(remaining, request)
			continue
		}
		accepted = append(accepted, answer)
	}
	return accepted, remaining
}

func IsValidCreditSuggestion(request crediting.CreditRequest, suggestion crediting.CreditSuggestion) bool {
	title, err := credits.NewTitle(suggestion.Title)
	if err != nil {
		return false
	}
	artists, err := credits.NewArtistCredit(suggestion.Artists)
	if err != nil {
		return false
	}
	return credits.NewCredit(title, artists).IsConsistentWith(request.Source)
}

func NewGenreSuggestionValidator(catalog genres.Pool) GenreValidator {
	return func(_ classification.GenreRequest, suggestion classification.GenreSuggestion) bool {
		_, err := catalog.NewClassification(genres.ClassificationDetails{
			Genre: suggestion.Genre, Subgenre: suggestion.Subgenre, IsBrazilian: suggestion.IsBrazilian, IsKnown: suggestion.IsKnown,
		})
		return err == nil
	}
}
