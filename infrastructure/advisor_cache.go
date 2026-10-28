package infrastructure

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/UrielJaloto/music-ops/services"
)

const (
	advisorCacheBucket = "advisor"
	advisorCacheTTL    = 365 * 24 * time.Hour
)

type CreditSuggester interface {
	SuggestCredits(ctx context.Context, requests []services.CreditRequest) ([]services.CreditSuggestion, error)
}

type GenreSuggester interface {
	SuggestGenres(ctx context.Context, requests []services.GenreRequest) ([]services.GenreSuggestion, error)
}

type CreditAdvisorCache struct {
	inner CreditSuggester
	cache ByteCache
	scope string
}

func NewCreditAdvisorCache(inner CreditSuggester, cache ByteCache, advisorNames string) CreditAdvisorCache {
	return CreditAdvisorCache{inner: inner, cache: cache, scope: "credit" + PromptFingerprint() + advisorNames}
}

func (decorator CreditAdvisorCache) SuggestCredits(ctx context.Context, requests []services.CreditRequest) ([]services.CreditSuggestion, error) {
	entry := cachedTask[services.CreditRequest, services.CreditSuggestion]{
		cache: decorator.cache, scope: decorator.scope, forward: decorator.inner.SuggestCredits,
		requestKey: func(request services.CreditRequest) string { return request.Key },
		identity:   func(request services.CreditRequest) any { return request.Source },
		answerKey:  func(suggestion services.CreditSuggestion) string { return suggestion.Key },
		withKey: func(suggestion services.CreditSuggestion, key string) services.CreditSuggestion {
			suggestion.Key = key
			return suggestion
		},
	}
	return entry.suggest(ctx, requests)
}

type GenreAdvisorCache struct {
	inner GenreSuggester
	cache ByteCache
	scope string
}

func NewGenreAdvisorCache(inner GenreSuggester, cache ByteCache, advisorNames string) GenreAdvisorCache {
	return GenreAdvisorCache{inner: inner, cache: cache, scope: "genre" + PromptFingerprint() + advisorNames}
}

type genreIdentity struct {
	Path    string   `json:"path"`
	Title   string   `json:"title"`
	Artists []string `json:"artists"`
	Tags    []string `json:"tags"`
}

func (decorator GenreAdvisorCache) SuggestGenres(ctx context.Context, requests []services.GenreRequest) ([]services.GenreSuggestion, error) {
	entry := cachedTask[services.GenreRequest, services.GenreSuggestion]{
		cache: decorator.cache, scope: decorator.scope, forward: decorator.inner.SuggestGenres,
		requestKey: func(request services.GenreRequest) string { return request.Key },
		identity: func(request services.GenreRequest) any {
			return genreIdentity{Path: request.Path, Title: request.Title, Artists: request.Artists, Tags: request.Tags}
		},
		answerKey: func(suggestion services.GenreSuggestion) string { return suggestion.Key },
		withKey: func(suggestion services.GenreSuggestion, key string) services.GenreSuggestion {
			suggestion.Key = key
			return suggestion
		},
	}
	return entry.suggest(ctx, requests)
}

type cachedTask[R, S any] struct {
	cache      ByteCache
	scope      string
	forward    func(ctx context.Context, requests []R) ([]S, error)
	requestKey func(R) string
	identity   func(R) any
	answerKey  func(S) string
	withKey    func(S, string) S
}

func (task cachedTask[R, S]) suggest(ctx context.Context, requests []R) ([]S, error) {
	found, err := task.lookup(ctx, requests)
	if err != nil {
		return nil, err
	}
	if len(found.misses) == 0 {
		return found.answers, nil
	}
	forwarded, err := task.forward(ctx, found.misses)
	if err != nil {
		return nil, err
	}
	if err := task.store(ctx, forwarded, found.cacheKeys); err != nil {
		return nil, err
	}
	return append(found.answers, forwarded...), nil
}

type cacheLookup[R, S any] struct {
	answers   []S
	misses    []R
	cacheKeys map[string]string
}

func (task cachedTask[R, S]) lookup(ctx context.Context, requests []R) (cacheLookup[R, S], error) {
	found := cacheLookup[R, S]{answers: []S{}, misses: []R{}, cacheKeys: map[string]string{}}
	for _, request := range requests {
		key, err := task.cacheKey(request)
		if err != nil {
			return found, err
		}
		stored, isHit, err := task.cache.Get(ctx, advisorCacheBucket, key)
		if err != nil {
			return found, err
		}
		if !isHit {
			found.misses = append(found.misses, request)
			found.cacheKeys[task.requestKey(request)] = key
			continue
		}
		answer, err := task.decode(stored, task.requestKey(request))
		if err != nil {
			return found, err
		}
		found.answers = append(found.answers, answer)
	}
	return found, nil
}

func (task cachedTask[R, S]) decode(stored []byte, requestKey string) (S, error) {
	var answer S
	if err := json.Unmarshal(stored, &answer); err != nil {
		return answer, fmt.Errorf("decoding cached advisor answer: %w", err)
	}
	return task.withKey(answer, requestKey), nil
}

func (task cachedTask[R, S]) store(ctx context.Context, answers []S, cacheKeys map[string]string) error {
	for _, answer := range answers {
		key, isRequested := cacheKeys[task.answerKey(answer)]
		if !isRequested {
			continue
		}
		encoded, err := json.Marshal(answer)
		if err != nil {
			return fmt.Errorf("encoding advisor answer: %w", err)
		}
		if err := task.cache.Put(ctx, advisorCacheBucket, key, encoded, advisorCacheTTL); err != nil {
			return err
		}
	}
	return nil
}

func (task cachedTask[R, S]) cacheKey(request R) (string, error) {
	canonical, err := json.Marshal(task.identity(request))
	if err != nil {
		return "", fmt.Errorf("encoding advisor request: %w", err)
	}
	digest := sha256.Sum256(append([]byte(task.scope), canonical...))
	return hex.EncodeToString(digest[:]), nil
}
