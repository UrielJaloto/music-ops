package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

const (
	processWaitDelay = 5 * time.Second
	stderrTailLimit  = 4096
	exitCodeUnknown  = -1
)

type ProcessRequest struct {
	Executable string
	Arguments  []string
	Stdin      io.Reader
	Stdout     io.Writer
}

type ProcessResult struct {
	ExitCode int
}

type ProcessError struct {
	Executable string
	ExitCode   int
	StderrTail string
}

func (processError ProcessError) Error() string {
	return fmt.Sprintf("%s exited with code %d: %s", processError.Executable, processError.ExitCode, processError.StderrTail)
}

type ProcessRunner struct{}

func NewProcessRunner() ProcessRunner {
	return ProcessRunner{}
}

func (runner ProcessRunner) Run(ctx context.Context, request ProcessRequest) (ProcessResult, error) {
	stderr := &tailBuffer{limit: stderrTailLimit}
	command := exec.CommandContext(ctx, request.Executable, request.Arguments...)
	command.Stdin = request.Stdin
	command.Stdout = request.Stdout
	command.Stderr = stderr
	command.WaitDelay = processWaitDelay
	hideProcessWindow(command)
	err := command.Run()
	if err == nil {
		return ProcessResult{ExitCode: 0}, nil
	}
	if ctx.Err() != nil {
		return ProcessResult{}, fmt.Errorf("running %s: %w", request.Executable, ctx.Err())
	}
	return ProcessResult{}, ProcessError{Executable: request.Executable, ExitCode: exitCodeOf(err), StderrTail: string(stderr.data)}
}

func exitCodeOf(err error) int {
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	return exitCodeUnknown
}

type tailBuffer struct {
	limit int
	data  []byte
}

func (buffer *tailBuffer) Write(chunk []byte) (int, error) {
	buffer.data = append(buffer.data, chunk...)
	if overflow := len(buffer.data) - buffer.limit; overflow > 0 {
		buffer.data = buffer.data[overflow:]
	}
	return len(chunk), nil
}
