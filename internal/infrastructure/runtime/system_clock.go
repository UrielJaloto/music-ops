package runtime

import (
	"crypto/rand"
	"encoding/hex"
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

type RandomRunIDs struct{}

func NewRandomRunIDs() RandomRunIDs {
	return RandomRunIDs{}
}

func (RandomRunIDs) NewRunID(startedAt time.Time) (runs.RunID, error) {
	suffix := make([]byte, runIDSuffixBytes)
	_, _ = rand.Read(suffix)
	return runs.NewRunID(startedAt, hex.EncodeToString(suffix))
}
