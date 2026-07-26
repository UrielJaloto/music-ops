package infrastructure

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/UrielJaloto/music-ops/domain"
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

func (RandomRunIDs) NewRunID(startedAt time.Time) (domain.RunID, error) {
	suffix := make([]byte, runIDSuffixBytes)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("generating run id: %w", err)
	}
	return domain.NewRunID(startedAt, hex.EncodeToString(suffix))
}
