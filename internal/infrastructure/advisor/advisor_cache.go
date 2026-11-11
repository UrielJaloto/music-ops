package advisor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/UrielJaloto/music-ops/internal/services/classification"
	"github.com/UrielJaloto/music-ops/internal/services/crediting"
)

const (
	CacheBucket     = "advisor"
	advisorCacheTTL = 365 * 24 * time.Hour
)

type CreditSuggester interface {
	SuggestCredits(ctx context.Context, requests []crediting.CreditRequest) ([]crediting.CreditSuggestion, error)
}

type GenreSuggester interface {
	SuggestGenres(ctx context.Context, requests []classification.GenreRequest) ([]classification.GenreSuggestion, error)
}

type CreditAdvisorCache struct {
	inner CreditSuggester
	cache AdvisorCacheStore
	scope string
}

func NewCreditAdvisorCache(inner CreditSuggester, cache AdvisorCacheStore, advisorNames string) CreditAdvisorCache {
	return CreditAdvisorCache{inner: inner, cache: cache, scope: "credit" + PromptFingerprint() + advisorNames}
}

func (decorator CreditAdvisorCache) SuggestCredits(ctx context.Context, requests []crediting.CreditRequest) ([]crediting.CreditSuggestion, error) {
	entry := cachedTask[crediting.CreditRequest, crediting.CreditSuggestion]{
		cache: decorator.cache, scope: decorator.scope, forward: decorator.inner.SuggestCredits,
		requestKey: func(request crediting.CreditRequest) string { return request.Key },
		identity:   func(request crediting.CreditRequest) any { return request.Source },
		answerKey:  func(suggestion crediting.CreditSuggestion) string { return suggestion.Key },
		withKey: func(suggestion crediting.CreditSuggestion, key string) crediting.CreditSuggestion {
			suggestion.Key = key
			return suggestion
		},
	}
	return entry.suggest(ctx, requests)
}

type GenreAdvisorCache struct {
	inner GenreSuggester
	cache AdvisorCacheStore
	scope string
}

func NewGenreAdvisorCache(inner GenreSuggester, cache AdvisorCacheStore, advisorNames string) GenreAdvisorCache {
	return GenreAdvisorCache{inner: inner, cache: cache, scope: "genre" + PromptFingerprint() + advisorNames}
}

type genreIdentity struct {
	Path    string   `json:"path"`
	Title   string   `json:"title"`
	Artists []string `json:"artists"`
	Tags    []string `json:"tags"`
}

func (decorator GenreAdvisorCache) SuggestGenres(ctx context.Context, requests []classification.GenreRequest) ([]classification.GenreSuggestion, error) {
	entry := cachedTask[classification.GenreRequest, classification.GenreSuggestion]{
		cache: decorator.cache, scope: decorator.scope, forward: decorator.inner.SuggestGenres,
		requestKey: func(request classification.GenreRequest) string { return request.Key },
		identity: func(request classification.GenreRequest) any {
			return genreIdentity{Path: request.Path, Title: request.Title, Artists: request.Artists, Tags: request.Tags}
		},
		answerKey: func(suggestion classification.GenreSuggestion) string { return suggestion.Key },
		withKey: func(suggestion classification.GenreSuggestion, key string) classification.GenreSuggestion {
			suggestion.Key = key
			return suggestion
		},
	}
	return entry.suggest(ctx, requests)
}

type cachedTask[R, S any] struct {
	cache      AdvisorCacheStore
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
		key := task.buildCacheKey(request)
		stored, isHit, err := task.cache.Get(ctx, key)
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
		encoded, _ := json.Marshal(answer)
		if err := task.cache.Put(ctx, key, encoded, advisorCacheTTL); err != nil {
			return err
		}
	}
	return nil
}

func (task cachedTask[R, S]) buildCacheKey(request R) string {
	canonical, _ := json.Marshal(task.identity(request))
	digest := sha256.Sum256(append([]byte(task.scope), canonical...))
	return hex.EncodeToString(digest[:])
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

type AdvisorCacheStore interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Put(ctx context.Context, key string, value []byte, ttl time.Duration) error
}
