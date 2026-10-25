package infrastructure

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

var processHelperPath string

func TestMain(m *testing.M) {
	os.Exit(runWithProcessHelper(m))
}

func runWithProcessHelper(m *testing.M) int {
	folder, err := os.MkdirTemp("", "music-ops-process-helper")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = os.RemoveAll(folder) }()
	processHelperPath = filepath.Join(folder, "processhelper.exe")
	build := exec.CommandContext(context.Background(), "go", "build", "-o", processHelperPath, "./testdata/processhelper")
	if output, err := build.CombinedOutput(); err != nil {
		fmt.Fprintln(os.Stderr, string(output), err)
		return 1
	}
	return m.Run()
}

func TestProcessRunner_Run_ExitCodes(t *testing.T) {
	cases := []struct {
		name         string
		arguments    []string
		expectedCode int
		isFailure    bool
	}{
		{name: "ExitZero_ReturnsResult", arguments: []string{"exit", "0"}, expectedCode: 0},
		{name: "ExitOne_ReturnsProcessError", arguments: []string{"exit", "1"}, expectedCode: 1, isFailure: true},
		{name: "ExitSeven_ReturnsProcessError", arguments: []string{"exit", "7"}, expectedCode: 7, isFailure: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := NewProcessRunner().Run(context.Background(), ProcessRequest{Executable: processHelperPath, Arguments: testCase.arguments})

			var processError ProcessError
			if testCase.isFailure != errors.As(err, &processError) {
				t.Fatalf("err = %v", err)
			}
			if testCase.isFailure && (processError.ExitCode != testCase.expectedCode || processError.Executable != processHelperPath) {
				t.Fatalf("process error = %+v", processError)
			}
			if !testCase.isFailure && result.ExitCode != testCase.expectedCode {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

func TestProcessRunner_Run_StderrTail(t *testing.T) {
	cases := []struct {
		name          string
		size          int
		expectedTail  int
		expectedStart string
	}{
		{name: "ShortStderr_KeptWhole", size: 10, expectedTail: 18, expectedStart: "HEAD"},
		{name: "StderrAtLimit_KeptWhole", size: stderrTailLimit - 8, expectedTail: stderrTailLimit, expectedStart: "HEAD"},
		{name: "StderrAboveLimit_TruncatedToTail", size: stderrTailLimit - 7, expectedTail: stderrTailLimit, expectedStart: "EADx"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := NewProcessRunner().Run(context.Background(), ProcessRequest{Executable: processHelperPath, Arguments: []string{"stderr", strconv.Itoa(testCase.size)}})

			var processError ProcessError
			if !errors.As(err, &processError) || processError.ExitCode != 3 {
				t.Fatalf("err = %v", err)
			}
			tail := processError.StderrTail
			if len(tail) != testCase.expectedTail || !strings.HasPrefix(tail, testCase.expectedStart) || !strings.HasSuffix(tail, "TAIL") {
				t.Fatalf("tail length = %d prefix = %q", len(tail), tail[:4])
			}
		})
	}
}

func TestProcessRunner_Run_PipesStdin(t *testing.T) {
	var stdout bytes.Buffer

	_, err := NewProcessRunner().Run(context.Background(), ProcessRequest{Executable: processHelperPath, Arguments: []string{"echo"}, Stdin: strings.NewReader("audio bytes"), Stdout: &stdout})

	if err != nil || stdout.String() != "audio bytes" {
		t.Fatalf("stdout = %q err = %v", stdout.String(), err)
	}
}

func TestProcessRunner_Run_CancellationKillsChild(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()

	_, err := NewProcessRunner().Run(ctx, ProcessRequest{Executable: processHelperPath, Arguments: []string{"sleep"}})

	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 10*time.Second {
		t.Fatalf("err = %v after %v", err, time.Since(started))
	}
}

func TestProcessRunner_Run_MissingExecutable(t *testing.T) {
	_, err := NewProcessRunner().Run(context.Background(), ProcessRequest{Executable: filepath.Join(t.TempDir(), "missing.exe")})

	var processError ProcessError
	if !errors.As(err, &processError) || processError.ExitCode != exitCodeUnknown {
		t.Fatalf("err = %v", err)
	}
}

func TestProcessError_Error(t *testing.T) {
	message := ProcessError{Executable: "ffmpeg", ExitCode: 2, StderrTail: "bad input"}.Error()

	if message != "ffmpeg exited with code 2: bad input" {
		t.Fatalf("message = %q", message)
	}
}
