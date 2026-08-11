package filesystem

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func differences(want, got any) string {
	if reflect.DeepEqual(want, got) {
		return ""
	}
	return fmt.Sprintf("want: %+v\ngot:  %+v", want, got)
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func TestOutputProbe_ProbeFileBlocked(t *testing.T) {
	output := t.TempDir()
	writeTestFile(t, filepath.Join(output, outputProbeName, "inside"), "x")

	report, err := NewOutputProbe(output).ProbeOutput(context.Background())

	if err != nil || report.IsWritable {
		t.Fatalf("report = %+v err = %v", report, err)
	}
}

func TestOutputProbe_InvalidPath(t *testing.T) {
	report, err := NewOutputProbe("in\x00valid").ProbeOutput(context.Background())

	if err == nil || report.IsWritable || !strings.Contains(err.Error(), "reading free space") {
		t.Fatalf("report = %+v err = %v", report, err)
	}
}
