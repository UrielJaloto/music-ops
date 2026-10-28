package infrastructure

import (
	"context"
	"strings"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
)

const advisorNameSeparator = ","

type ChainedAdvisor interface {
	SuggestCredits(ctx context.Context, requests []services.CreditRequest) ([]services.CreditSuggestion, error)
	SuggestGenres(ctx context.Context, requests []services.GenreRequest) ([]services.GenreSuggestion, error)
	Name() string
	ProbeAdvisor(ctx context.Context) services.AdvisorReport
}

type CreditValidator func(request services.CreditRequest, suggestion services.CreditSuggestion) bool

type GenreValidator func(request services.GenreRequest, suggestion services.GenreSuggestion) bool

type AdvisorChain struct {
	advisors    []ChainedAdvisor
	validCredit CreditValidator
	validGenre  GenreValidator
}

func NewAdvisorChain(advisors []ChainedAdvisor, validCredit CreditValidator, validGenre GenreValidator) AdvisorChain {
	return AdvisorChain{advisors: advisors, validCredit: validCredit, validGenre: validGenre}
}

func (chain AdvisorChain) SuggestCredits(ctx context.Context, requests []services.CreditRequest) ([]services.CreditSuggestion, error) {
	steps := make([]chainStep[services.CreditRequest, services.CreditSuggestion], 0, len(chain.advisors))
	for _, advisor := range chain.advisors {
		steps = append(steps, advisor.SuggestCredits)
	}
	link := chainLink[services.CreditRequest, services.CreditSuggestion]{
		requestKey: func(request services.CreditRequest) string { return request.Key },
		answerKey:  func(suggestion services.CreditSuggestion) string { return suggestion.Key },
		isValid:    chain.validCredit,
	}
	return runChain(ctx, steps, link, requests), nil
}

func (chain AdvisorChain) SuggestGenres(ctx context.Context, requests []services.GenreRequest) ([]services.GenreSuggestion, error) {
	steps := make([]chainStep[services.GenreRequest, services.GenreSuggestion], 0, len(chain.advisors))
	for _, advisor := range chain.advisors {
		steps = append(steps, advisor.SuggestGenres)
	}
	link := chainLink[services.GenreRequest, services.GenreSuggestion]{
		requestKey: func(request services.GenreRequest) string { return request.Key },
		answerKey:  func(suggestion services.GenreSuggestion) string { return suggestion.Key },
		isValid:    chain.validGenre,
	}
	return runChain(ctx, steps, link, requests), nil
}

func (chain AdvisorChain) ProbeAdvisors(ctx context.Context) []services.AdvisorReport {
	reports := make([]services.AdvisorReport, 0, len(chain.advisors))
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

func ValidCreditSuggestion(request services.CreditRequest, suggestion services.CreditSuggestion) bool {
	title, err := domain.NewTitle(suggestion.Title)
	if err != nil {
		return false
	}
	artists, err := domain.NewArtistCredit(suggestion.Artists)
	if err != nil {
		return false
	}
	return domain.NewCredit(title, artists).IsConsistentWith(request.Source)
}

func ValidGenreSuggestion(_ services.GenreRequest, suggestion services.GenreSuggestion) bool {
	_, err := domain.NewClassification(domain.ClassificationDetails{
		Genre: suggestion.Genre, Subgenre: suggestion.Subgenre, IsBrazilian: suggestion.IsBrazilian, IsKnown: suggestion.IsKnown,
	})
	return err == nil
}
