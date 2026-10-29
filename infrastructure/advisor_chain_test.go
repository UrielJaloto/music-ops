package infrastructure

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
)

type scriptedAdvisor struct {
	name          string
	credits       map[string]services.CreditSuggestion
	genres        map[string]services.GenreSuggestion
	err           error
	creditBatches [][]string
	genreBatches  [][]string
}

func (advisor *scriptedAdvisor) SuggestCredits(_ context.Context, requests []services.CreditRequest) ([]services.CreditSuggestion, error) {
	keys := make([]string, 0, len(requests))
	answers := []services.CreditSuggestion{}
	for _, request := range requests {
		keys = append(keys, request.Key)
		if answer, isKnown := advisor.credits[request.Key]; isKnown {
			answers = append(answers, answer)
		}
	}
	advisor.creditBatches = append(advisor.creditBatches, keys)
	return answers, advisor.err
}

func (advisor *scriptedAdvisor) SuggestGenres(_ context.Context, requests []services.GenreRequest) ([]services.GenreSuggestion, error) {
	keys := make([]string, 0, len(requests))
	answers := []services.GenreSuggestion{}
	for _, request := range requests {
		keys = append(keys, request.Key)
		if answer, isKnown := advisor.genres[request.Key]; isKnown {
			answers = append(answers, answer)
		}
	}
	advisor.genreBatches = append(advisor.genreBatches, keys)
	return answers, advisor.err
}

func (advisor *scriptedAdvisor) Name() string { return advisor.name }

func (advisor *scriptedAdvisor) ProbeAdvisor(context.Context) services.AdvisorReport {
	return services.AdvisorReport{Name: advisor.name, IsAvailable: advisor.err == nil}
}

func songRequests() []services.CreditRequest {
	return []services.CreditRequest{
		{Key: "a", Source: domain.CreditSource{Title: "Song A (feat. X)", Artist: "Main"}},
		{Key: "b", Source: domain.CreditSource{Title: "Song B", Artist: "Main"}},
	}
}

func goodCredit(key, title string, artists ...string) services.CreditSuggestion {
	return services.CreditSuggestion{Key: key, Title: title, Artists: artists}
}

func TestAdvisorChain_SuggestCredits(t *testing.T) {
	answerA := goodCredit("a", "Song A", "Main", "X")
	answerB := goodCredit("b", "Song B", "Main")
	invented := goodCredit("b", "Song B", "Main", "Invented Star")
	cases := []struct {
		name          string
		first         *scriptedAdvisor
		second        *scriptedAdvisor
		expected      []services.CreditSuggestion
		secondBatches [][]string
	}{
		{name: "FirstAnswersAll", first: &scriptedAdvisor{credits: map[string]services.CreditSuggestion{"a": answerA, "b": answerB}}, second: &scriptedAdvisor{}, expected: []services.CreditSuggestion{answerA, answerB}},
		{name: "FirstFails_SecondUsed", first: &scriptedAdvisor{err: ErrAdvisorUnavailable}, second: &scriptedAdvisor{credits: map[string]services.CreditSuggestion{"a": answerA, "b": answerB}}, expected: []services.CreditSuggestion{answerA, answerB}, secondBatches: [][]string{{"a", "b"}}},
		{name: "PartialAnswer_OnlyMissingForwarded", first: &scriptedAdvisor{credits: map[string]services.CreditSuggestion{"a": answerA}}, second: &scriptedAdvisor{credits: map[string]services.CreditSuggestion{"b": answerB}}, expected: []services.CreditSuggestion{answerA, answerB}, secondBatches: [][]string{{"b"}}},
		{name: "RejectedItemForwarded", first: &scriptedAdvisor{credits: map[string]services.CreditSuggestion{"a": answerA, "b": invented}}, second: &scriptedAdvisor{credits: map[string]services.CreditSuggestion{"b": answerB}}, expected: []services.CreditSuggestion{answerA, answerB}, secondBatches: [][]string{{"b"}}},
		{name: "AllFail_EmptyWithoutError", first: &scriptedAdvisor{err: ErrAdvisorUnavailable}, second: &scriptedAdvisor{err: ErrAdvisorAnswerMalformed}, expected: []services.CreditSuggestion{}, secondBatches: [][]string{{"a", "b"}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			chain := NewAdvisorChain([]ChainedAdvisor{testCase.first, testCase.second}, ValidCreditSuggestion, ValidGenreSuggestion)

			suggestions, err := chain.SuggestCredits(context.Background(), songRequests())

			if diff := cmp.Diff(testCase.expected, suggestions); diff != "" || err != nil {
				t.Fatalf("suggestions (-want +got):\n%s err = %v", diff, err)
			}
			if diff := cmp.Diff(testCase.secondBatches, testCase.second.creditBatches); diff != "" {
				t.Fatalf("second advisor batches (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAdvisorChain_SuggestGenres_RejectsInvalidGenre(t *testing.T) {
	invalid := services.GenreSuggestion{Key: "g1", Genre: "Polka", IsKnown: true}
	valid := services.GenreSuggestion{Key: "g1", Genre: "Rock", Subgenre: "Metal", IsKnown: true}
	first := &scriptedAdvisor{genres: map[string]services.GenreSuggestion{"g1": invalid}}
	second := &scriptedAdvisor{genres: map[string]services.GenreSuggestion{"g1": valid}}
	chain := NewAdvisorChain([]ChainedAdvisor{first, second}, ValidCreditSuggestion, ValidGenreSuggestion)

	suggestions, err := chain.SuggestGenres(context.Background(), []services.GenreRequest{{Key: "g1"}})

	if diff := cmp.Diff([]services.GenreSuggestion{valid}, suggestions); diff != "" || err != nil {
		t.Fatalf("suggestions (-want +got):\n%s err = %v", diff, err)
	}
}

func TestAdvisorChain_CancelledStopsChain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	first := &scriptedAdvisor{}

	suggestions, err := NewAdvisorChain([]ChainedAdvisor{first}, ValidCreditSuggestion, ValidGenreSuggestion).SuggestCredits(ctx, songRequests())

	if err != nil || len(suggestions) != 0 || len(first.creditBatches) != 0 {
		t.Fatalf("suggestions = %v batches = %v", suggestions, first.creditBatches)
	}
}

func TestAdvisorChain_ProbeAndNames(t *testing.T) {
	chain := NewAdvisorChain([]ChainedAdvisor{&scriptedAdvisor{name: "local:qwen"}, &scriptedAdvisor{name: "local:backup", err: errors.New("down")}}, ValidCreditSuggestion, ValidGenreSuggestion)

	reports := chain.ProbeAdvisors(context.Background())

	expected := []services.AdvisorReport{{Name: "local:qwen", IsAvailable: true}, {Name: "local:backup"}}
	if diff := cmp.Diff(expected, reports); diff != "" || chain.Names() != "local:qwen,local:backup" {
		t.Fatalf("reports (-want +got):\n%s names = %s", diff, chain.Names())
	}
}

func TestValidators(t *testing.T) {
	request := songRequests()[0]

	if !ValidCreditSuggestion(request, goodCredit("a", "Song A", "Main", "X")) || ValidCreditSuggestion(request, goodCredit("a", "", "Main")) || ValidCreditSuggestion(request, goodCredit("a", "Song A")) {
		t.Fatal("credit validator")
	}
	if !ValidGenreSuggestion(services.GenreRequest{}, services.GenreSuggestion{IsKnown: false, Genre: "x"}) || ValidGenreSuggestion(services.GenreRequest{}, services.GenreSuggestion{IsKnown: true, Genre: "x"}) {
		t.Fatal("genre validator")
	}
}

func TestCreditAdvisorCache(t *testing.T) {
	inner := &scriptedAdvisor{credits: map[string]services.CreditSuggestion{"a": goodCredit("a", "Song A", "Main", "X"), "b": goodCredit("b", "Song B", "Main")}}
	cache := newMemoryByteCache()
	decorator := NewCreditAdvisorCache(inner, cache, "local:qwen")

	first, firstErr := decorator.SuggestCredits(context.Background(), songRequests())
	renamed := []services.CreditRequest{{Key: "other-a", Source: songRequests()[0].Source}}
	second, secondErr := decorator.SuggestCredits(context.Background(), renamed)

	if firstErr != nil || secondErr != nil || len(first) != 2 || len(inner.creditBatches) != 1 {
		t.Fatalf("first = %v batches = %v errors = %v %v", first, inner.creditBatches, firstErr, secondErr)
	}
	if diff := cmp.Diff([]services.CreditSuggestion{goodCredit("other-a", "Song A", "Main", "X")}, second); diff != "" {
		t.Fatalf("second (-want +got):\n%s", diff)
	}
	for key := range cache.ttls {
		if cache.ttls[key] != advisorCacheTTL {
			t.Fatalf("ttl = %v", cache.ttls[key])
		}
	}
}

func TestGenreAdvisorCache_PartialHits(t *testing.T) {
	rock := services.GenreSuggestion{Key: "g1", Genre: "Rock", IsKnown: true}
	pop := services.GenreSuggestion{Key: "g2", Genre: "Pop", IsKnown: true}
	inner := &scriptedAdvisor{genres: map[string]services.GenreSuggestion{"g1": rock, "g2": pop}}
	decorator := NewGenreAdvisorCache(inner, newMemoryByteCache(), "local:qwen")
	first := services.GenreRequest{Key: "g1", Title: "Iron Man", Artists: []string{"Black Sabbath"}}
	second := services.GenreRequest{Key: "g2", Title: "Thriller", Artists: []string{"Michael Jackson"}}

	_, warmErr := decorator.SuggestGenres(context.Background(), []services.GenreRequest{first})
	suggestions, err := decorator.SuggestGenres(context.Background(), []services.GenreRequest{first, second})

	if warmErr != nil || err != nil || len(suggestions) != 2 || len(inner.genreBatches) != 2 || len(inner.genreBatches[1]) != 1 || inner.genreBatches[1][0] != "g2" {
		t.Fatalf("suggestions = %v batches = %v", suggestions, inner.genreBatches)
	}
}

func TestAdvisorCache_ScopeChangesKey(t *testing.T) {
	cache := newMemoryByteCache()
	inner := &scriptedAdvisor{credits: map[string]services.CreditSuggestion{"a": goodCredit("a", "Song A", "Main", "X"), "b": goodCredit("b", "Song B", "Main")}}

	_, _ = NewCreditAdvisorCache(inner, cache, "local:qwen").SuggestCredits(context.Background(), songRequests())
	_, _ = NewCreditAdvisorCache(inner, cache, "local:other").SuggestCredits(context.Background(), songRequests())

	if len(inner.creditBatches) != 2 || len(cache.values) != 4 {
		t.Fatalf("batches = %v cached = %d", inner.creditBatches, len(cache.values))
	}
}

func TestAdvisorCache_Failures(t *testing.T) {
	errStore := errors.New("store down")
	failing := newMemoryByteCache()
	failing.err = errStore
	corrupt := newMemoryByteCache()
	inner := &scriptedAdvisor{err: ErrAdvisorUnavailable}

	_, storeErr := NewCreditAdvisorCache(inner, failing, "x").SuggestCredits(context.Background(), songRequests())
	_, innerErr := NewGenreAdvisorCache(inner, corrupt, "x").SuggestGenres(context.Background(), []services.GenreRequest{{Key: "g"}})
	key, _ := cachedTask[services.GenreRequest, services.GenreSuggestion]{scope: "genre" + PromptFingerprint() + "x", identity: func(request services.GenreRequest) any {
		return genreIdentity{Path: request.Path, Title: request.Title, Artists: request.Artists, Tags: request.Tags}
	}}.cacheKey(services.GenreRequest{Key: "g"})
	corrupt.values[advisorCacheBucket+"/"+key] = []byte("{")
	_, decodeErr := NewGenreAdvisorCache(inner, corrupt, "x").SuggestGenres(context.Background(), []services.GenreRequest{{Key: "g"}})

	if !errors.Is(storeErr, errStore) || !errors.Is(innerErr, ErrAdvisorUnavailable) || decodeErr == nil {
		t.Fatalf("errors = %v %v %v", storeErr, innerErr, decodeErr)
	}
}
