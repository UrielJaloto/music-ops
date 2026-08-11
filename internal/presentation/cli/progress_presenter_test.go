package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
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
