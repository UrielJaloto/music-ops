package infrastructure

import (
	"context"
	"errors"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/domain"
)

func TestWorkerPool_RunParallel_HonorsLimit(t *testing.T) {
	cases := []struct {
		limit int32
		size  int32
	}{
		{limit: 1, size: 8},
		{limit: 2, size: 8},
		{limit: 4, size: 3},
	}
	for _, testCase := range cases {
		t.Run("Limit"+strconv.Itoa(int(testCase.limit)), func(t *testing.T) {
			var running, peak, done atomic.Int32
			gate := make(chan struct{})
			release := sync.OnceFunc(func() { close(gate) })
			expectedPeak := min(testCase.limit, testCase.size)

			err := NewWorkerPool(int(testCase.limit)).RunParallel(context.Background(), int(testCase.size), func(context.Context, int) error {
				current := running.Add(1)
				recordPeak(&peak, current)
				if current == expectedPeak {
					time.AfterFunc(50*time.Millisecond, release)
				}
				<-gate
				running.Add(-1)
				done.Add(1)
				return nil
			})

			if err != nil || peak.Load() != expectedPeak || done.Load() != testCase.size {
				t.Fatalf("peak = %d done = %d err = %v", peak.Load(), done.Load(), err)
			}
		})
	}
}

func recordPeak(peak *atomic.Int32, current int32) {
	for {
		previous := peak.Load()
		if current <= previous || peak.CompareAndSwap(previous, current) {
			return
		}
	}
}

func TestWorkerPool_RunParallel_ErrorWaitsForStartedTasks(t *testing.T) {
	errTask := errors.New("task failed")
	var finished atomic.Int32

	err := NewWorkerPool(2).RunParallel(context.Background(), 2, func(ctx context.Context, index int) error {
		if index == 0 {
			return errTask
		}
		<-ctx.Done()
		finished.Add(1)
		return nil
	})

	if !errors.Is(err, errTask) || finished.Load() != 1 {
		t.Fatalf("err = %v finished = %d", err, finished.Load())
	}
}

func TestWorkerPool_RunParallel_Cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32

	err := NewWorkerPool(1).RunParallel(ctx, 5, func(context.Context, int) error {
		calls.Add(1)
		return nil
	})

	if !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("err = %v calls = %d", err, calls.Load())
	}
}

func TestWorkerPool_RunParallel_SizeZero(t *testing.T) {
	var calls atomic.Int32

	err := NewWorkerPool(2).RunParallel(context.Background(), 0, func(context.Context, int) error {
		calls.Add(1)
		return nil
	})

	if err != nil || calls.Load() != 0 {
		t.Fatalf("err = %v calls = %d", err, calls.Load())
	}
}

func TestWorkerPool_EffectiveLimit(t *testing.T) {
	cases := []struct {
		name     string
		limit    int
		expected int
	}{
		{name: "Zero_UsesCPUCountMinusOne", limit: 0, expected: max(runtime.NumCPU()-1, 1)},
		{name: "Negative_UsesCPUCountMinusOne", limit: -1, expected: max(runtime.NumCPU()-1, 1)},
		{name: "One_KeepsOne", limit: 1, expected: 1},
		{name: "Five_KeepsFive", limit: 5, expected: 5},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if limit := NewWorkerPool(testCase.limit).effectiveLimit(); limit != testCase.expected {
				t.Fatalf("limit = %d", limit)
			}
		})
	}
}

func TestRandomRunIDs_NewRunID_Format(t *testing.T) {
	startedAt := time.Date(2026, 10, 16, 8, 30, 5, 0, time.UTC)

	id, err := NewRandomRunIDs().NewRunID(startedAt)

	if err != nil || !regexp.MustCompile(`^20261016-083005-[0-9a-f]{4}$`).MatchString(string(id)) {
		t.Fatalf("id = %q err = %v", id, err)
	}
}

func TestSystemClock_Now_IsCurrentTime(t *testing.T) {
	before := time.Now()

	now := NewSystemClock().Now()

	if now.Before(before) || now.After(time.Now()) {
		t.Fatalf("now = %v", now)
	}
}

type importProgressSpy struct{ started []string }

func (spy *importProgressSpy) StageStarted(stage string, _ int) {
	spy.started = append(spy.started, stage)
}

func (spy *importProgressSpy) TrackFinished(string, string, string) {}

func (spy *importProgressSpy) StageFinished(string) {}

type fixedImportClock struct{ now time.Time }

func (clock fixedImportClock) Now() time.Time { return clock.now }

type fixedImportRunIDs struct{ id domain.RunID }

func (ids fixedImportRunIDs) NewRunID(time.Time) (domain.RunID, error) { return ids.id, nil }

func TestImportRuntime_DelegatesToParts(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	progress := &importProgressSpy{}
	importRuntime := NewImportRuntime(NewWorkerPool(1), fixedImportClock{now: now}, fixedImportRunIDs{id: "run-1"}, progress)
	var calls atomic.Int32

	err := importRuntime.RunParallel(context.Background(), 3, func(context.Context, int) error {
		calls.Add(1)
		return nil
	})
	id, idErr := importRuntime.NewRunID(now)
	importRuntime.Progress().StageStarted("convert", 1)

	if err != nil || idErr != nil || calls.Load() != 3 || id != "run-1" || !importRuntime.Now().Equal(now) || len(progress.started) != 1 {
		t.Fatalf("calls = %d id = %q err = %v %v progress = %v", calls.Load(), id, err, idErr, progress.started)
	}
}

func TestWorkerPool_Run_DelegatesToRunParallel(t *testing.T) {
	var calls atomic.Int32

	err := NewWorkerPool(2).Run(context.Background(), 4, func(context.Context, int) error {
		calls.Add(1)
		return nil
	})

	if err != nil || calls.Load() != 4 {
		t.Fatalf("calls = %d err = %v", calls.Load(), err)
	}
}
