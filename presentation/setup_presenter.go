package presentation

import (
	"fmt"
	"io"
	"sync"

	"github.com/UrielJaloto/music-ops/services"
)

const (
	percentStep      = 10
	fullPercent      = 100
	bytesPerMegabyte = 1 << 20
)

type SetupPresenter struct {
	output  io.Writer
	errors  ErrorReporter
	mutex   *sync.Mutex
	sizes   map[string]int64
	printed map[string]int
}

func NewSetupPresenter(output io.Writer, errors ErrorReporter) SetupPresenter {
	return SetupPresenter{output: output, errors: errors, mutex: &sync.Mutex{}, sizes: map[string]int64{}, printed: map[string]int{}}
}

func (presenter SetupPresenter) ArtifactStarted(name string, sizeBytes int64) {
	presenter.mutex.Lock()
	defer presenter.mutex.Unlock()
	presenter.sizes[name] = sizeBytes
	presenter.printed[name] = -percentStep
	presenter.write(fmt.Sprintf("downloading %s (%d MiB)\n", name, sizeBytes/bytesPerMegabyte))
}

func (presenter SetupPresenter) ArtifactProgressed(name string, doneBytes int64) {
	presenter.mutex.Lock()
	defer presenter.mutex.Unlock()
	size := presenter.sizes[name]
	if size <= 0 {
		return
	}
	percent := int(min(doneBytes*fullPercent/size, fullPercent))
	if percent < presenter.printed[name]+percentStep {
		return
	}
	presenter.printed[name] = percent - percent%percentStep
	presenter.write(fmt.Sprintf("  %s %3d%%  %s\n", progressBar(percent, fullPercent), percent, name))
}

func (presenter SetupPresenter) ArtifactFinished(name string) {
	presenter.mutex.Lock()
	defer presenter.mutex.Unlock()
	presenter.write("finished " + name + "\n")
}

func (presenter SetupPresenter) Installed(output services.InstalledAdvisorOutput) int {
	presenter.write("advisor installed\n  server: " + output.ServerPath + "\n  model:  " + output.ModelPath + "\n")
	return exitSuccess
}

func (presenter SetupPresenter) Usage(text string) int {
	presenter.write(text + "\n")
	return exitFailure
}

func (presenter SetupPresenter) Failed(err error) int {
	return presenter.errors.Present(err)
}

func (presenter SetupPresenter) write(text string) {
	_, _ = io.WriteString(presenter.output, text)
}
