package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	lastFMCacheBucket   = "lastfm"
	lastFMFoundTTL      = 30 * 24 * time.Hour
	lastFMEmptyTTL      = 3 * 24 * time.Hour
	lastFMKeySeparator  = "\x1f"
	lastFMCacheTrackKey = "track"
	lastFMCacheArtist   = "artist"
)

type TagSource interface {
	FetchTrackTags(ctx context.Context, artist, title string) ([]string, error)
	FetchArtistTags(ctx context.Context, artist string) ([]string, error)
}

type ByteCache interface {
	Get(ctx context.Context, bucket, key string) ([]byte, bool, error)
	Put(ctx context.Context, bucket, key string, value []byte, ttl time.Duration) error
}

type LastFMCache struct {
	inner TagSource
	cache ByteCache
}

func NewLastFMCache(inner TagSource, cache ByteCache) LastFMCache {
	return LastFMCache{inner: inner, cache: cache}
}

func (decorator LastFMCache) FetchTrackTags(ctx context.Context, artist, title string) ([]string, error) {
	key := buildCacheKey(lastFMCacheTrackKey, artist, title)
	return decorator.readCached(ctx, key, func() ([]string, error) { return decorator.inner.FetchTrackTags(ctx, artist, title) })
}

func (decorator LastFMCache) FetchArtistTags(ctx context.Context, artist string) ([]string, error) {
	key := buildCacheKey(lastFMCacheArtist, artist)
	return decorator.readCached(ctx, key, func() ([]string, error) { return decorator.inner.FetchArtistTags(ctx, artist) })
}

func (decorator LastFMCache) readCached(ctx context.Context, key string, fetch func() ([]string, error)) ([]string, error) {
	stored, isHit, err := decorator.cache.Get(ctx, lastFMCacheBucket, key)
	if err != nil {
		return nil, err
	}
	if isHit {
		return decodeCachedTags(stored)
	}
	tags, err := fetch()
	if err != nil {
		return nil, err
	}
	return tags, decorator.store(ctx, key, tags)
}

func (decorator LastFMCache) store(ctx context.Context, key string, tags []string) error {
	encoded, err := json.Marshal(tags)
	if err != nil {
		return fmt.Errorf("encoding cached tags: %w", err)
	}
	ttl := lastFMFoundTTL
	if len(tags) == 0 {
		ttl = lastFMEmptyTTL
	}
	return decorator.cache.Put(ctx, lastFMCacheBucket, key, encoded, ttl)
}

func decodeCachedTags(stored []byte) ([]string, error) {
	tags := []string{}
	if err := json.Unmarshal(stored, &tags); err != nil {
		return nil, fmt.Errorf("decoding cached tags: %w", err)
	}
	return tags, nil
}

func buildCacheKey(parts ...string) string {
	folded := make([]string, 0, len(parts))
	for _, part := range parts {
		folded = append(folded, strings.ToLower(strings.TrimSpace(part)))
	}
	return strings.Join(folded, lastFMKeySeparator)
}
