package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/UrielJaloto/music-ops/internal/services/importing"
	bolt "go.etcd.io/bbolt"
	boltErrors "go.etcd.io/bbolt/errors"
)

const (
	boltFilePermission = 0o600
	boltLockTimeout    = 2 * time.Second
)

type BoltStore struct {
	path     string
	mutex    sync.RWMutex
	database *bolt.DB
}

func NewBoltStore(path string) *BoltStore {
	return &BoltStore{path: path}
}

func (store *BoltStore) Open() error {
	database, err := bolt.Open(store.path, boltFilePermission, &bolt.Options{Timeout: boltLockTimeout})
	if errors.Is(err, boltErrors.ErrTimeout) {
		return fmt.Errorf("%w: %s", importing.ErrRunInProgress, store.path)
	}
	if err != nil {
		return fmt.Errorf("opening store %s: %w", store.path, err)
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.database = database
	return nil
}

func (store *BoltStore) Close() error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.database == nil {
		return nil
	}
	err := store.database.Close()
	store.database = nil
	return err
}

func (store *BoltStore) openDatabase(ctx context.Context) (*bolt.DB, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.database == nil {
		return nil, importing.ErrWorkspaceClosed
	}
	return store.database, nil
}

func (store *BoltStore) read(ctx context.Context, bucket, key string) ([]byte, bool, error) {
	database, err := store.openDatabase(ctx)
	if err != nil {
		return nil, false, err
	}
	var value []byte
	err = database.View(func(transaction *bolt.Tx) error {
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
	database, err := store.openDatabase(ctx)
	if err != nil {
		return err
	}
	err = database.Update(func(transaction *bolt.Tx) error {
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
	database, err := store.openDatabase(ctx)
	if err != nil {
		return err
	}
	err = database.Update(func(transaction *bolt.Tx) error {
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
	store  *BoltStore
	clock  CacheClock
	bucket string
}

func NewBoltCache(store *BoltStore, clock CacheClock, bucket string) BoltCache {
	return BoltCache{store: store, clock: clock, bucket: bucket}
}

type cacheEnvelope struct {
	Value     []byte    `json:"value"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (cache BoltCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	bucket := cache.bucket
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

func (cache BoltCache) Put(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	encoded, _ := json.Marshal(cacheEnvelope{Value: value, ExpiresAt: cache.clock.Now().Add(ttl)})
	return cache.store.write(ctx, cache.bucket, key, encoded)
}

const stagingFilePermission = 0o600

const stagingFolderPermission = 0o750
