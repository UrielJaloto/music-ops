package advisor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/UrielJaloto/music-ops/internal/infrastructure/process"
	"github.com/UrielJaloto/music-ops/internal/services/classification"
	"github.com/UrielJaloto/music-ops/internal/services/crediting"
	"github.com/UrielJaloto/music-ops/internal/services/diagnostics"
)

const (
	llamaGPULayers      = 99
	llamaContextSize    = 8192
	llamaHost           = "127.0.0.1"
	llamaHealthInterval = 250 * time.Millisecond
	llamaStartTimeout   = 120 * time.Second
	llamaStopWait       = 10 * time.Second
	notInstalledModel   = "not-installed"
)

var ErrLocalAdvisorStartTimeout = errors.New("local advisor did not become ready in time")

type LlamaServerSettings struct {
	Executable string
	ModelFile  string
	Port       int
	HTTP       *http.Client
	Clock      AdvisorClock
}

func (settings LlamaServerSettings) buildBaseURL() string {
	return BuildLocalAdvisorURL(settings.Port)
}

type LlamaServer struct {
	runner   LlamaProcessRunner
	settings LlamaServerSettings
}

func NewLlamaServer(runner LlamaProcessRunner, settings LlamaServerSettings) LlamaServer {
	return LlamaServer{runner: runner, settings: settings}
}

type LlamaServerSession struct {
	stop func(ctx context.Context) error
}

func (session LlamaServerSession) Stop(ctx context.Context) error {
	if session.stop == nil {
		return nil
	}
	return session.stop(ctx)
}

func (server LlamaServer) Start(ctx context.Context) (LlamaServerSession, error) {
	if server.isHealthy(ctx) {
		return LlamaServerSession{}, nil
	}
	if !isFile(server.settings.Executable) || !isFile(server.settings.ModelFile) {
		return LlamaServerSession{}, diagnostics.ErrLocalAdvisorNotInstalled
	}
	processContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
	child := &childProcess{done: make(chan struct{})}
	go func() {
		_, child.err = server.runner.Run(processContext, process.ProcessRequest{Executable: server.settings.Executable, Arguments: server.buildArguments()})
		close(child.done)
	}()
	session := LlamaServerSession{stop: stopChild(cancel, child)}
	if err := server.waitUntilHealthy(ctx, child); err != nil {
		return LlamaServerSession{}, errors.Join(err, session.Stop(context.WithoutCancel(ctx)))
	}
	return session, nil
}

func (server LlamaServer) buildArguments() []string {
	return []string{
		"-m", server.settings.ModelFile, "-ngl", strconv.Itoa(llamaGPULayers), "-c", strconv.Itoa(llamaContextSize),
		"--port", strconv.Itoa(server.settings.Port), "--host", llamaHost, "--jinja", "-fa", "on", "--parallel", "1",
	}
}

type childProcess struct {
	done chan struct{}
	err  error
}

func (server LlamaServer) waitUntilHealthy(ctx context.Context, child *childProcess) error {
	for range int(llamaStartTimeout / llamaHealthInterval) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-child.done:
			return fmt.Errorf("local advisor exited during start: %w", child.err)
		case <-server.settings.Clock.After(llamaHealthInterval):
		}
		if server.isHealthy(ctx) {
			return nil
		}
	}
	return ErrLocalAdvisorStartTimeout
}

func (server LlamaServer) isHealthy(ctx context.Context) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.settings.buildBaseURL()+healthPath, nil)
	if err != nil {
		return false
	}
	response, err := server.settings.HTTP.Do(request)
	if err != nil {
		return false
	}
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode == http.StatusOK
}

func stopChild(cancel context.CancelFunc, child *childProcess) func(ctx context.Context) error {
	var once sync.Once
	return func(ctx context.Context) error {
		var err error
		once.Do(func() {
			cancel()
			err = awaitExit(ctx, child)
		})
		return err
	}
}

func awaitExit(ctx context.Context, child *childProcess) error {
	timeout, cancel := context.WithTimeout(ctx, llamaStopWait)
	defer cancel()
	select {
	case <-child.done:
		return nil
	case <-timeout.Done():
		return fmt.Errorf("stopping local advisor: %w", timeout.Err())
	}
}

type LocalAdvisor interface {
	SuggestCredits(ctx context.Context, requests []crediting.CreditRequest) ([]crediting.CreditSuggestion, error)
	SuggestGenres(ctx context.Context, requests []classification.GenreRequest) ([]classification.GenreSuggestion, error)
	Name() string
	ProbeAdvisor(ctx context.Context) diagnostics.AdvisorReport
}

type AdvisorServer interface {
	Start(ctx context.Context) (LlamaServerSession, error)
}

type lazyServerState struct {
	mutex     sync.Mutex
	isStarted bool
	session   LlamaServerSession
}

type LazyLlamaAdvisor struct {
	inner  LocalAdvisor
	server AdvisorServer
	state  *lazyServerState
}

func NewLazyLlamaAdvisor(inner LocalAdvisor, server AdvisorServer) LazyLlamaAdvisor {
	return LazyLlamaAdvisor{inner: inner, server: server, state: &lazyServerState{}}
}

func (advisor LazyLlamaAdvisor) SuggestCredits(ctx context.Context, requests []crediting.CreditRequest) ([]crediting.CreditSuggestion, error) {
	if err := advisor.ensureStarted(ctx); err != nil {
		return nil, err
	}
	return advisor.inner.SuggestCredits(ctx, requests)
}

func (advisor LazyLlamaAdvisor) SuggestGenres(ctx context.Context, requests []classification.GenreRequest) ([]classification.GenreSuggestion, error) {
	if err := advisor.ensureStarted(ctx); err != nil {
		return nil, err
	}
	return advisor.inner.SuggestGenres(ctx, requests)
}

func (advisor LazyLlamaAdvisor) Name() string {
	return advisor.inner.Name()
}

func (advisor LazyLlamaAdvisor) ProbeAdvisor(ctx context.Context) diagnostics.AdvisorReport {
	if err := advisor.ensureStarted(ctx); err != nil {
		return diagnostics.AdvisorReport{Name: advisor.Name(), Detail: err.Error()}
	}
	return advisor.inner.ProbeAdvisor(ctx)
}

func (advisor LazyLlamaAdvisor) Close(ctx context.Context) error {
	advisor.state.mutex.Lock()
	defer advisor.state.mutex.Unlock()
	if !advisor.state.isStarted {
		return nil
	}
	advisor.state.isStarted = false
	return advisor.state.session.Stop(ctx)
}

func (advisor LazyLlamaAdvisor) ensureStarted(ctx context.Context) error {
	advisor.state.mutex.Lock()
	defer advisor.state.mutex.Unlock()
	if advisor.state.isStarted {
		return nil
	}
	session, err := advisor.server.Start(ctx)
	if err != nil {
		return err
	}
	advisor.state.session = session
	advisor.state.isStarted = true
	return nil
}

func BuildLocalAdvisorURL(port int) string {
	return "http://" + llamaHost + ":" + strconv.Itoa(port)
}

func ExtractModelName(modelFile string) string {
	if modelFile == "" {
		return notInstalledModel
	}
	return filepath.Base(modelFile)
}

type LlamaProcessRunner interface {
	Run(ctx context.Context, request process.ProcessRequest) (process.ProcessResult, error)
}
