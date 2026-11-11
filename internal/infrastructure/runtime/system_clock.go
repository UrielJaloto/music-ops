package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/UrielJaloto/music-ops/internal/domain/runs"
)

const runIDSuffixBytes = 2

type SystemClock struct{}

func NewSystemClock() SystemClock {
	return SystemClock{}
}

func (SystemClock) Now() time.Time {
	return time.Now()
}

func (SystemClock) After(duration time.Duration) <-chan time.Time {
	return time.After(duration)
}

type RandomRunIDs struct {
	mutex    sync.Mutex
	reserved runs.RunID
}

func NewRandomRunIDs() *RandomRunIDs {
	return &RandomRunIDs{}
}

func (ids *RandomRunIDs) ReserveRunID(startedAt time.Time) (runs.RunID, error) {
	id, err := generateRunID(startedAt)
	ids.mutex.Lock()
	defer ids.mutex.Unlock()
	ids.reserved = id
	return id, err
}

func (ids *RandomRunIDs) NewRunID(startedAt time.Time) (runs.RunID, error) {
	ids.mutex.Lock()
	reserved := ids.reserved
	ids.reserved = ""
	ids.mutex.Unlock()
	if reserved != "" {
		return reserved, nil
	}
	return generateRunID(startedAt)
}

func generateRunID(startedAt time.Time) (runs.RunID, error) {
	suffix := make([]byte, runIDSuffixBytes)
	_, _ = rand.Read(suffix)
	return runs.NewRunID(startedAt, hex.EncodeToString(suffix))
}
