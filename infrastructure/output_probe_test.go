package infrastructure

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestOutputProbe_ProbeOutput(t *testing.T) {
	output := filepath.Join(t.TempDir(), "library")

	report, err := NewOutputProbe(output).ProbeOutput(context.Background())

	if err != nil || report.Path != output || !report.IsWritable || report.FreeBytes == 0 || pathExists(filepath.Join(output, outputProbeName)) {
		t.Fatalf("report = %+v err = %v", report, err)
	}
}

func TestOutputProbe_NotWritable(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	writeTestFile(t, blocker, "x")

	report, err := NewOutputProbe(filepath.Join(blocker, "library")).ProbeOutput(context.Background())

	if report.IsWritable || err == nil {
		t.Fatalf("report = %+v err = %v", report, err)
	}
}

func TestOutputProbe_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewOutputProbe(t.TempDir()).ProbeOutput(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
