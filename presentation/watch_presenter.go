package presentation

import (
	"fmt"
	"io"
	"sync"
)

type WatchPresenter struct {
	output io.Writer
	errors ErrorReporter
	mutex  *sync.Mutex
}

func NewWatchPresenter(output io.Writer, errors ErrorReporter) WatchPresenter {
	return WatchPresenter{output: output, errors: errors, mutex: &sync.Mutex{}}
}

func (presenter WatchPresenter) FolderImported(folder string, exitStatus int) {
	presenter.write(fmt.Sprintf("imported %s (exit %d)\n", folder, exitStatus))
}

func (presenter WatchPresenter) FolderFailed(folder, reason string) {
	presenter.write(fmt.Sprintf("failed %s: %s\n", folder, reason))
}

func (presenter WatchPresenter) Stopped() int {
	presenter.write("watch stopped\n")
	return exitSuccess
}

func (presenter WatchPresenter) Usage(text string) int {
	presenter.write(text + "\n")
	return exitFailure
}

func (presenter WatchPresenter) Failed(err error) int {
	return presenter.errors.Present(err)
}

func (presenter WatchPresenter) write(text string) {
	presenter.mutex.Lock()
	defer presenter.mutex.Unlock()
	_, _ = io.WriteString(presenter.output, text)
}
