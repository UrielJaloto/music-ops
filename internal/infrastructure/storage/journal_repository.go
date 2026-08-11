package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/UrielJaloto/music-ops/internal/domain/tracks"
)

const (
	convertedBucket = "journal_converted"
	publishedBucket = "journal_published"
)

type JournalRepository struct {
	store *BoltStore
}

func NewJournalRepository(store *BoltStore) JournalRepository {
	return JournalRepository{store: store}
}

type convertedRecord struct {
	Staged string `json:"staged"`
}

type publishedRecord struct {
	Target string `json:"target"`
}

func (repository JournalRepository) IsConverted(ctx context.Context, id tracks.TrackID) (bool, error) {
	var record convertedRecord
	isFound, err := repository.load(ctx, convertedBucket, id, &record)
	return isFound && record.Staged != "", err
}

func (repository JournalRepository) RecordConverted(ctx context.Context, id tracks.TrackID, staged string) error {
	return repository.save(ctx, convertedBucket, id, convertedRecord{Staged: staged})
}

func (repository JournalRepository) IsPublished(ctx context.Context, id tracks.TrackID) (string, bool, error) {
	var record publishedRecord
	isFound, err := repository.load(ctx, publishedBucket, id, &record)
	if err != nil || !isFound {
		return "", false, err
	}
	return record.Target, true, nil
}

func (repository JournalRepository) RecordPublished(ctx context.Context, id tracks.TrackID, target string) error {
	return repository.save(ctx, publishedBucket, id, publishedRecord{Target: target})
}

func (repository JournalRepository) load(ctx context.Context, bucket string, id tracks.TrackID, record any) (bool, error) {
	stored, isFound, err := repository.store.read(ctx, bucket, string(id))
	if err != nil || !isFound {
		return false, err
	}
	if err := json.Unmarshal(stored, record); err != nil {
		return false, fmt.Errorf("decoding journal record %s: %w", id, err)
	}
	return true, nil
}

func (repository JournalRepository) save(ctx context.Context, bucket string, id tracks.TrackID, record any) error {
	encoded, _ := json.Marshal(record)
	return repository.store.write(ctx, bucket, string(id), encoded)
}
