package infrastructure

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

type memoryByteCache struct {
	values map[string][]byte
	ttls   map[string]time.Duration
	err    error
}

func newMemoryByteCache() *memoryByteCache {
	return &memoryByteCache{values: map[string][]byte{}, ttls: map[string]time.Duration{}}
}

func (cache *memoryByteCache) Get(_ context.Context, bucket, key string) ([]byte, bool, error) {
	value, isFound := cache.values[bucket+"/"+key]
	return value, isFound, cache.err
}

func (cache *memoryByteCache) Put(_ context.Context, bucket, key string, value []byte, ttl time.Duration) error {
	cache.values[bucket+"/"+key] = value
	cache.ttls[bucket+"/"+key] = ttl
	return nil
}

type tagSourceSpy struct {
	tags  []string
	err   error
	calls int
}

func (spy *tagSourceSpy) TrackTags(context.Context, string, string) ([]string, error) {
	spy.calls++
	return spy.tags, spy.err
}

func (spy *tagSourceSpy) ArtistTags(context.Context, string) ([]string, error) {
	spy.calls++
	return spy.tags, spy.err
}

func TestLastFMCache_HitMakesNoCalls(t *testing.T) {
	inner := &tagSourceSpy{tags: []string{"rock"}}
	cache := newMemoryByteCache()
	decorator := NewLastFMCache(inner, cache)

	first, firstErr := decorator.TrackTags(context.Background(), "AC/DC", "T.N.T.")
	second, secondErr := decorator.TrackTags(context.Background(), " ac/dc ", "t.n.t.")
	artist, artistErr := decorator.ArtistTags(context.Background(), "AC/DC")
	artistAgain, _ := decorator.ArtistTags(context.Background(), "ac/dc")

	if firstErr != nil || secondErr != nil || artistErr != nil || inner.calls != 2 {
		t.Fatalf("calls = %d errors = %v %v %v", inner.calls, firstErr, secondErr, artistErr)
	}
	if diff := cmp.Diff([][]string{{"rock"}, {"rock"}, {"rock"}, {"rock"}}, [][]string{first, second, artist, artistAgain}); diff != "" {
		t.Fatalf("results (-want +got):\n%s", diff)
	}
	if cache.ttls["lastfm/track\x1fac/dc\x1ft.n.t."] != 30*24*time.Hour {
		t.Fatalf("ttls = %v", cache.ttls)
	}
}

func TestLastFMCache_EmptyResultHasShorterTTL(t *testing.T) {
	cache := newMemoryByteCache()
	decorator := NewLastFMCache(&tagSourceSpy{tags: []string{}}, cache)

	tags, err := decorator.ArtistTags(context.Background(), "Unknown")

	if err != nil || len(tags) != 0 || cache.ttls["lastfm/artist\x1funknown"] != 3*24*time.Hour {
		t.Fatalf("tags = %v ttls = %v err = %v", tags, cache.ttls, err)
	}
}

func TestLastFMCache_ErrorsNotCached(t *testing.T) {
	errNetwork := errors.New("network down")
	inner := &tagSourceSpy{err: errNetwork}
	cache := newMemoryByteCache()
	decorator := NewLastFMCache(inner, cache)

	_, firstErr := decorator.ArtistTags(context.Background(), "A")
	_, secondErr := decorator.ArtistTags(context.Background(), "A")

	if !errors.Is(firstErr, errNetwork) || !errors.Is(secondErr, errNetwork) || inner.calls != 2 || len(cache.values) != 0 {
		t.Fatalf("calls = %d cached = %v", inner.calls, cache.values)
	}
}

func TestLastFMCache_CacheFailures(t *testing.T) {
	errStore := errors.New("store broken")
	failing := newMemoryByteCache()
	failing.err = errStore
	corrupt := newMemoryByteCache()
	corrupt.values["lastfm/artist\x1fa"] = []byte("{")

	_, getErr := NewLastFMCache(&tagSourceSpy{}, failing).ArtistTags(context.Background(), "A")
	_, decodeErr := NewLastFMCache(&tagSourceSpy{}, corrupt).ArtistTags(context.Background(), "A")

	if !errors.Is(getErr, errStore) || decodeErr == nil {
		t.Fatalf("errors = %v %v", getErr, decodeErr)
	}
}
