package advisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/internal/domain/credits"
	"github.com/UrielJaloto/music-ops/internal/infrastructure/process"
	"github.com/UrielJaloto/music-ops/internal/infrastructure/transcoding"
	"github.com/UrielJaloto/music-ops/internal/services/classification"
	"github.com/UrielJaloto/music-ops/internal/services/crediting"
	"github.com/UrielJaloto/music-ops/internal/services/diagnostics"
)

type scriptedAdvisor struct {
	name          string
	credits       map[string]crediting.CreditSuggestion
	genres        map[string]classification.GenreSuggestion
	err           error
	creditBatches [][]string
	genreBatches  [][]string
}

func (advisor *scriptedAdvisor) SuggestCredits(_ context.Context, requests []crediting.CreditRequest) ([]crediting.CreditSuggestion, error) {
	keys := make([]string, 0, len(requests))
	answers := []crediting.CreditSuggestion{}
	for _, request := range requests {
		keys = append(keys, request.Key)
		if answer, isKnown := advisor.credits[request.Key]; isKnown {
			answers = append(answers, answer)
		}
	}
	advisor.creditBatches = append(advisor.creditBatches, keys)
	return answers, advisor.err
}

func (advisor *scriptedAdvisor) SuggestGenres(_ context.Context, requests []classification.GenreRequest) ([]classification.GenreSuggestion, error) {
	keys := make([]string, 0, len(requests))
	answers := []classification.GenreSuggestion{}
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

func (advisor *scriptedAdvisor) ProbeAdvisor(context.Context) diagnostics.AdvisorReport {
	return diagnostics.AdvisorReport{Name: advisor.name, IsAvailable: advisor.err == nil}
}

func songRequests() []crediting.CreditRequest {
	return []crediting.CreditRequest{
		{Key: "a", Source: credits.CreditSource{Title: "Song A (feat. X)", Artist: "Main"}},
		{Key: "b", Source: credits.CreditSource{Title: "Song B", Artist: "Main"}},
	}
}

func goodCredit(key, title string, artists ...string) crediting.CreditSuggestion {
	return crediting.CreditSuggestion{Key: key, Title: title, Artists: artists}
}

func TestAdvisorChain_SuggestCredits(t *testing.T) {
	answerA := goodCredit("a", "Song A", "Main", "X")
	answerB := goodCredit("b", "Song B", "Main")
	invented := goodCredit("b", "Song B", "Main", "Invented Star")
	cases := []struct {
		name          string
		first         *scriptedAdvisor
		second        *scriptedAdvisor
		expected      []crediting.CreditSuggestion
		secondBatches [][]string
	}{
		{name: "FirstAnswersAll", first: &scriptedAdvisor{credits: map[string]crediting.CreditSuggestion{"a": answerA, "b": answerB}}, second: &scriptedAdvisor{}, expected: []crediting.CreditSuggestion{answerA, answerB}},
		{name: "FirstFails_SecondUsed", first: &scriptedAdvisor{err: ErrAdvisorUnavailable}, second: &scriptedAdvisor{credits: map[string]crediting.CreditSuggestion{"a": answerA, "b": answerB}}, expected: []crediting.CreditSuggestion{answerA, answerB}, secondBatches: [][]string{{"a", "b"}}},
		{name: "PartialAnswer_OnlyMissingForwarded", first: &scriptedAdvisor{credits: map[string]crediting.CreditSuggestion{"a": answerA}}, second: &scriptedAdvisor{credits: map[string]crediting.CreditSuggestion{"b": answerB}}, expected: []crediting.CreditSuggestion{answerA, answerB}, secondBatches: [][]string{{"b"}}},
		{name: "RejectedItemForwarded", first: &scriptedAdvisor{credits: map[string]crediting.CreditSuggestion{"a": answerA, "b": invented}}, second: &scriptedAdvisor{credits: map[string]crediting.CreditSuggestion{"b": answerB}}, expected: []crediting.CreditSuggestion{answerA, answerB}, secondBatches: [][]string{{"b"}}},
		{name: "AllFail_EmptyWithoutError", first: &scriptedAdvisor{err: ErrAdvisorUnavailable}, second: &scriptedAdvisor{err: ErrAdvisorAnswerMalformed}, expected: []crediting.CreditSuggestion{}, secondBatches: [][]string{{"a", "b"}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			chain := NewAdvisorChain([]ChainedAdvisor{testCase.first, testCase.second}, IsValidCreditSuggestion, ValidGenreSuggestion)

			suggestions, err := chain.SuggestCredits(context.Background(), songRequests())

			if diff := differences(testCase.expected, suggestions); diff != "" || err != nil {
				t.Fatalf("suggestions (-want +got):\n%s err = %v", diff, err)
			}
			if diff := differences(testCase.secondBatches, testCase.second.creditBatches); diff != "" {
				t.Fatalf("second advisor batches (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAdvisorChain_SuggestGenres_RejectsInvalidGenre(t *testing.T) {
	invalid := classification.GenreSuggestion{Key: "g1", Genre: "Polka", IsKnown: true}
	valid := classification.GenreSuggestion{Key: "g1", Genre: "Rock", Subgenre: "Metal", IsKnown: true}
	first := &scriptedAdvisor{genres: map[string]classification.GenreSuggestion{"g1": invalid}}
	second := &scriptedAdvisor{genres: map[string]classification.GenreSuggestion{"g1": valid}}
	chain := NewAdvisorChain([]ChainedAdvisor{first, second}, IsValidCreditSuggestion, ValidGenreSuggestion)

	suggestions, err := chain.SuggestGenres(context.Background(), []classification.GenreRequest{{Key: "g1"}})

	if diff := differences([]classification.GenreSuggestion{valid}, suggestions); diff != "" || err != nil {
		t.Fatalf("suggestions (-want +got):\n%s err = %v", diff, err)
	}
}

func TestAdvisorChain_CancelledStopsChain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	first := &scriptedAdvisor{}

	suggestions, err := NewAdvisorChain([]ChainedAdvisor{first}, IsValidCreditSuggestion, ValidGenreSuggestion).SuggestCredits(ctx, songRequests())

	if err != nil || len(suggestions) != 0 || len(first.creditBatches) != 0 {
		t.Fatalf("suggestions = %v batches = %v", suggestions, first.creditBatches)
	}
}

func TestAdvisorChain_ProbeAndNames(t *testing.T) {
	chain := NewAdvisorChain([]ChainedAdvisor{&scriptedAdvisor{name: "local:qwen"}, &scriptedAdvisor{name: "local:backup", err: errors.New("down")}}, IsValidCreditSuggestion, ValidGenreSuggestion)

	reports := chain.ProbeAdvisors(context.Background())

	expected := []diagnostics.AdvisorReport{{Name: "local:qwen", IsAvailable: true}, {Name: "local:backup"}}
	if diff := differences(expected, reports); diff != "" || chain.Names() != "local:qwen,local:backup" {
		t.Fatalf("reports (-want +got):\n%s names = %s", diff, chain.Names())
	}
}

func TestValidators(t *testing.T) {
	request := songRequests()[0]

	if !IsValidCreditSuggestion(request, goodCredit("a", "Song A", "Main", "X")) || IsValidCreditSuggestion(request, goodCredit("a", "", "Main")) || IsValidCreditSuggestion(request, goodCredit("a", "Song A")) {
		t.Fatal("credit validator")
	}
	if !ValidGenreSuggestion(classification.GenreRequest{}, classification.GenreSuggestion{IsKnown: false, Genre: "x"}) || ValidGenreSuggestion(classification.GenreRequest{}, classification.GenreSuggestion{IsKnown: true, Genre: "x"}) {
		t.Fatal("genre validator")
	}
}

func TestCreditAdvisorCache(t *testing.T) {
	inner := &scriptedAdvisor{credits: map[string]crediting.CreditSuggestion{"a": goodCredit("a", "Song A", "Main", "X"), "b": goodCredit("b", "Song B", "Main")}}
	cache := newMemoryByteCache()
	decorator := NewCreditAdvisorCache(inner, cache, "local:qwen")

	first, firstErr := decorator.SuggestCredits(context.Background(), songRequests())
	renamed := []crediting.CreditRequest{{Key: "other-a", Source: songRequests()[0].Source}}
	second, secondErr := decorator.SuggestCredits(context.Background(), renamed)

	if firstErr != nil || secondErr != nil || len(first) != 2 || len(inner.creditBatches) != 1 {
		t.Fatalf("first = %v batches = %v errors = %v %v", first, inner.creditBatches, firstErr, secondErr)
	}
	if diff := differences([]crediting.CreditSuggestion{goodCredit("other-a", "Song A", "Main", "X")}, second); diff != "" {
		t.Fatalf("second (-want +got):\n%s", diff)
	}
	for key := range cache.ttls {
		if cache.ttls[key] != advisorCacheTTL {
			t.Fatalf("ttl = %v", cache.ttls[key])
		}
	}
}

func TestGenreAdvisorCache_PartialHits(t *testing.T) {
	rock := classification.GenreSuggestion{Key: "g1", Genre: "Rock", IsKnown: true}
	pop := classification.GenreSuggestion{Key: "g2", Genre: "Pop", IsKnown: true}
	inner := &scriptedAdvisor{genres: map[string]classification.GenreSuggestion{"g1": rock, "g2": pop}}
	decorator := NewGenreAdvisorCache(inner, newMemoryByteCache(), "local:qwen")
	first := classification.GenreRequest{Key: "g1", Title: "Iron Man", Artists: []string{"Black Sabbath"}}
	second := classification.GenreRequest{Key: "g2", Title: "Thriller", Artists: []string{"Michael Jackson"}}

	_, warmErr := decorator.SuggestGenres(context.Background(), []classification.GenreRequest{first})
	suggestions, err := decorator.SuggestGenres(context.Background(), []classification.GenreRequest{first, second})

	if warmErr != nil || err != nil || len(suggestions) != 2 || len(inner.genreBatches) != 2 || len(inner.genreBatches[1]) != 1 || inner.genreBatches[1][0] != "g2" {
		t.Fatalf("suggestions = %v batches = %v", suggestions, inner.genreBatches)
	}
}

func TestAdvisorCache_ScopeChangesKey(t *testing.T) {
	cache := newMemoryByteCache()
	inner := &scriptedAdvisor{credits: map[string]crediting.CreditSuggestion{"a": goodCredit("a", "Song A", "Main", "X"), "b": goodCredit("b", "Song B", "Main")}}

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
	_, innerErr := NewGenreAdvisorCache(inner, corrupt, "x").SuggestGenres(context.Background(), []classification.GenreRequest{{Key: "g"}})
	key := cachedTask[classification.GenreRequest, classification.GenreSuggestion]{scope: "genre" + PromptFingerprint() + "x", identity: func(request classification.GenreRequest) any {
		return genreIdentity{Path: request.Path, Title: request.Title, Artists: request.Artists, Tags: request.Tags}
	}}.buildCacheKey(classification.GenreRequest{Key: "g"})
	corrupt.values[key] = []byte("{")
	_, decodeErr := NewGenreAdvisorCache(inner, corrupt, "x").SuggestGenres(context.Background(), []classification.GenreRequest{{Key: "g"}})

	if !errors.Is(storeErr, errStore) || !errors.Is(innerErr, ErrAdvisorUnavailable) || decodeErr == nil {
		t.Fatalf("errors = %v %v %v", storeErr, innerErr, decodeErr)
	}
}

func differences(want, got any) string {
	if reflect.DeepEqual(want, got) {
		return ""
	}
	return fmt.Sprintf("want: %+v\ngot:  %+v", want, got)
}

type memoryByteCache struct {
	values map[string][]byte
	ttls   map[string]time.Duration
	err    error
	putErr error
}

func newMemoryByteCache() *memoryByteCache {
	return &memoryByteCache{values: map[string][]byte{}, ttls: map[string]time.Duration{}}
}

func (cache *memoryByteCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	value, isFound := cache.values[key]
	return value, isFound, cache.err
}

func (cache *memoryByteCache) Put(_ context.Context, key string, value []byte, ttl time.Duration) error {
	cache.values[key] = value
	cache.ttls[key] = ttl
	return cache.putErr
}

const fixtureCacheFolder = "music-ops-test-cache"

func requireFFmpeg(t *testing.T) (string, string) {
	t.Helper()
	ffmpeg, ffmpegErr := transcoding.LocateFFmpeg("", exec.LookPath)
	ffprobe, ffprobeErr := transcoding.LocateFFprobe("", exec.LookPath)
	if ffmpegErr == nil && ffprobeErr == nil {
		return ffmpeg, ffprobe
	}
	if strict, _ := os.LookupEnv("MUSIC_OPS_STRICT"); strict == "1" {
		t.Fatalf("ffmpeg is required: %v %v", ffmpegErr, ffprobeErr)
	}
	t.Skipf("ffmpeg is not installed: %v %v", ffmpegErr, ffprobeErr)
	return "", ""
}

type firingClock struct{ requested []time.Duration }

func (clock *firingClock) After(duration time.Duration) <-chan time.Time {
	clock.requested = append(clock.requested, duration)
	fired := make(chan time.Time, 1)
	fired <- time.Time{}
	return fired
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

type scriptedRunner struct {
	requests []process.ProcessRequest
}

func (runner *scriptedRunner) Run(_ context.Context, request process.ProcessRequest) (process.ProcessResult, error) {
	runner.requests = append(runner.requests, request)
	return process.ProcessResult{}, nil
}
