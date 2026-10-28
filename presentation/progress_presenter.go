package presentation

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/UrielJaloto/music-ops/services"
)

const (
	progressBarWidth   = 10
	progressFilled     = "█"
	progressEmpty      = "░"
	progressRedrawGap  = 100 * time.Millisecond
	progressFailedName = "failed"
	carriageReturn     = "\r"
	clearToEndOfLine   = "\x1b[K"
	secondsPerMinute   = 60
)

var stageOrder = []string{services.StageDiscover, services.StageConvert, services.StageMetadata, services.StagePublish}

var stageLabels = map[string]string{
	services.StageDiscover: "Discovering",
	services.StageConvert:  "Converting",
	services.StageMetadata: "Resolving",
	services.StagePublish:  "Publishing",
}

type ProgressClock interface {
	Now() time.Time
}

type stageProgress struct {
	name      string
	total     int
	done      int
	failed    int
	startedAt time.Time
}

type ProgressPresenter struct {
	output io.Writer
	clock  ProgressClock
	mutex  *sync.Mutex
	state  *progressState
}

type progressState struct {
	stage      stageProgress
	lastDrawAt time.Time
	isTerminal bool
	isDetected bool
}

func NewProgressPresenter(output io.Writer, clock ProgressClock) ProgressPresenter {
	return ProgressPresenter{output: output, clock: clock, mutex: &sync.Mutex{}, state: &progressState{}}
}

func (presenter ProgressPresenter) StageStarted(stage string, total int) {
	presenter.mutex.Lock()
	defer presenter.mutex.Unlock()
	now := presenter.clock.Now()
	presenter.state.stage = stageProgress{name: stage, total: total, startedAt: now}
	presenter.draw(now)
}

func (presenter ProgressPresenter) TrackFinished(stage, _ string, status string) {
	presenter.mutex.Lock()
	defer presenter.mutex.Unlock()
	if presenter.state.stage.name != stage {
		return
	}
	presenter.state.stage.done++
	if status == progressFailedName {
		presenter.state.stage.failed++
	}
	now := presenter.clock.Now()
	if now.Sub(presenter.state.lastDrawAt) >= progressRedrawGap {
		presenter.draw(now)
	}
}

func (presenter ProgressPresenter) StageFinished(stage string) {
	presenter.mutex.Lock()
	defer presenter.mutex.Unlock()
	if presenter.state.stage.name != stage {
		return
	}
	presenter.draw(presenter.clock.Now())
	if presenter.terminal() {
		presenter.write("\n")
	}
}

func (presenter ProgressPresenter) draw(now time.Time) {
	presenter.state.lastDrawAt = now
	line := renderStageLine(presenter.state.stage, now)
	if presenter.terminal() {
		presenter.write(carriageReturn + line + clearToEndOfLine)
		return
	}
	presenter.write(line + "\n")
}

func (presenter ProgressPresenter) terminal() bool {
	if !presenter.state.isDetected {
		presenter.state.isTerminal = isCharacterDevice(presenter.output)
		presenter.state.isDetected = true
	}
	return presenter.state.isTerminal
}

func (presenter ProgressPresenter) write(text string) {
	_, _ = io.WriteString(presenter.output, text)
}

func isCharacterDevice(output io.Writer) bool {
	file, isFile := output.(*os.File)
	if !isFile {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func renderStageLine(stage stageProgress, now time.Time) string {
	line := fmt.Sprintf("[%d/%d] %-11s %s  %d/%d", stageNumber(stage.name), len(stageOrder), stageLabel(stage.name), progressBar(stage.done, stage.total), stage.done, stage.total)
	if stage.failed > 0 {
		line += fmt.Sprintf("  %d failed", stage.failed)
	}
	return line + "  " + clockText(now.Sub(stage.startedAt))
}

func stageNumber(stage string) int {
	for index, name := range stageOrder {
		if name == stage {
			return index + 1
		}
	}
	return 0
}

func stageLabel(stage string) string {
	if label, isKnown := stageLabels[stage]; isKnown {
		return label
	}
	return stage
}

func progressBar(done, total int) string {
	filled := progressBarWidth
	if total > 0 {
		filled = min(done*progressBarWidth/total, progressBarWidth)
	}
	return strings.Repeat(progressFilled, filled) + strings.Repeat(progressEmpty, progressBarWidth-filled)
}

func clockText(elapsed time.Duration) string {
	seconds := int(elapsed.Seconds())
	return fmt.Sprintf("%02d:%02d", seconds/secondsPerMinute, seconds%secondsPerMinute)
}
