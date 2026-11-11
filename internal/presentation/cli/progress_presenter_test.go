package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/internal/services/importing"
)

func TestProgressPresenter_Terminal_RedrawsOneLine(t *testing.T) {
	var output bytes.Buffer
	presenter := NewProgressPresenter(&output, &steppedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), step: time.Second})
	presenter.state.isDetected, presenter.state.isTerminal = true, true

	presenter.StageStarted("custom", 1)
	presenter.StageFinished("custom")

	lines := strings.Split(output.String(), carriageReturn)
	if len(lines) != 3 || lines[0] != "" || !strings.HasPrefix(lines[1], "[0/4] custom ") || !strings.HasSuffix(lines[1], clearToEndOfLine) || !strings.HasSuffix(lines[2], clearToEndOfLine+"\n") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestProgressPresenter_Terminal_TicksWhileStageIsOpen(t *testing.T) {
	var output bytes.Buffer
	clock := &steppedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), step: time.Second, ticks: make(chan time.Time), afterCalls: make(chan struct{})}
	presenter := NewProgressPresenter(&output, clock)
	presenter.state.isDetected, presenter.state.isTerminal = true, true

	presenter.StageStarted(importing.StageConvert, 2)
	<-clock.afterCalls
	before := strings.Count(output.String(), carriageReturn)
	clock.ticks <- time.Time{}
	<-clock.afterCalls

	if after := strings.Count(output.String(), carriageReturn); after != before+1 || !strings.Contains(output.String(), "Converting") {
		t.Fatalf("draws = %d then %d output = %q", before, after, output.String())
	}
}

func TestProgressPresenter_Tick_StopsAfterStageFinished(t *testing.T) {
	var output bytes.Buffer
	clock := &steppedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), step: time.Second, ticks: make(chan time.Time, 1)}
	presenter := NewProgressPresenter(&output, clock)
	presenter.state.isTicking = true
	clock.ticks <- time.Time{}

	presenter.tick()

	if presenter.state.isTicking || output.Len() != 0 {
		t.Fatalf("ticking = %v output = %q", presenter.state.isTicking, output.String())
	}
}

func TestProgressPresenter_NotTerminal_DoesNotTick(t *testing.T) {
	var output bytes.Buffer
	presenter := NewProgressPresenter(&output, &steppedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), step: time.Second})
	presenter.state.isDetected = true

	presenter.StageStarted(importing.StageDiscover, 0)

	if presenter.state.isTicking || !strings.HasSuffix(output.String(), "\n") {
		t.Fatalf("ticking = %v output = %q", presenter.state.isTicking, output.String())
	}
}
