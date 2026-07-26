package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
	boltErrors "go.etcd.io/bbolt/errors"

	"github.com/UrielJaloto/music-ops/services"
)

const (
	boltFilePermission = 0o600
	boltLockTimeout    = 2 * time.Second
)

type BoltStore struct {
	database *bolt.DB
}

func OpenBoltStore(path string) (*BoltStore, error) {
	database, err := bolt.Open(path, boltFilePermission, &bolt.Options{Timeout: boltLockTimeout})
	if errors.Is(err, boltErrors.ErrTimeout) {
		return nil, fmt.Errorf("%w: %s", services.ErrLibraryBusy, path)
	}
	if err != nil {
		return nil, fmt.Errorf("opening store %s: %w", path, err)
	}
	return &BoltStore{database: database}, nil
}

func (store *BoltStore) Close() error {
	return store.database.Close()
}

func (store *BoltStore) read(ctx context.Context, bucket, key string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	var value []byte
	err := store.database.View(func(transaction *bolt.Tx) error {
		existing := transaction.Bucket([]byte(bucket))
		if existing == nil {
			return nil
		}
		if stored := existing.Get([]byte(key)); stored != nil {
			value = append([]byte{}, stored...)
		}
		return nil
	})
	if err != nil {
		return nil, false, fmt.Errorf("reading %s/%s: %w", bucket, key, err)
	}
	return value, value != nil, nil
}

func (store *BoltStore) write(ctx context.Context, bucket, key string, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := store.database.Update(func(transaction *bolt.Tx) error {
		existing, err := transaction.CreateBucketIfNotExists([]byte(bucket))
		if err != nil {
			return err
		}
		return existing.Put([]byte(key), value)
	})
	if err != nil {
		return fmt.Errorf("writing %s/%s: %w", bucket, key, err)
	}
	return nil
}

func (store *BoltStore) delete(ctx context.Context, bucket, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := store.database.Update(func(transaction *bolt.Tx) error {
		existing := transaction.Bucket([]byte(bucket))
		if existing == nil {
			return nil
		}
		return existing.Delete([]byte(key))
	})
	if err != nil {
		return fmt.Errorf("deleting %s/%s: %w", bucket, key, err)
	}
	return nil
}

type CacheClock interface {
	Now() time.Time
}

type BoltCache struct {
	store *BoltStore
	clock CacheClock
}

func NewBoltCache(store *BoltStore, clock CacheClock) BoltCache {
	return BoltCache{store: store, clock: clock}
}

type cacheEnvelope struct {
	Value     []byte    `json:"value"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (cache BoltCache) Get(ctx context.Context, bucket, key string) ([]byte, bool, error) {
	stored, isFound, err := cache.store.read(ctx, bucket, key)
	if err != nil || !isFound {
		return nil, false, err
	}
	var envelope cacheEnvelope
	if err := json.Unmarshal(stored, &envelope); err != nil {
		return nil, false, fmt.Errorf("decoding cached %s/%s: %w", bucket, key, err)
	}
	if !cache.clock.Now().Before(envelope.ExpiresAt) {
		return nil, false, cache.store.delete(ctx, bucket, key)
	}
	return envelope.Value, true, nil
}

func (cache BoltCache) Put(ctx context.Context, bucket, key string, value []byte, ttl time.Duration) error {
	encoded, err := json.Marshal(cacheEnvelope{Value: value, ExpiresAt: cache.clock.Now().Add(ttl)})
	if err != nil {
		return fmt.Errorf("encoding cached %s/%s: %w", bucket, key, err)
	}
	return cache.store.write(ctx, bucket, key, encoded)
}
