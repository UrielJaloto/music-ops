package infrastructure

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/services"
)

func portOf(t *testing.T, address string) int {
	t.Helper()
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port
}

func installedFiles(t *testing.T, modelName string) (string, string) {
	t.Helper()
	model := filepath.Join(t.TempDir(), modelName)
	writeTestFile(t, model, "gguf")
	return fakeLlamaServerPath, model
}

func TestLlamaServer_ReusesRunningServer(t *testing.T) {
	running := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusOK) }))
	defer running.Close()
	runner := &scriptedRunner{}
	server := NewLlamaServer(runner, LlamaServerSettings{Port: portOf(t, running.URL), HTTP: running.Client(), Clock: &firingClock{}})

	session, err := server.Start(context.Background())

	if err != nil || len(runner.requests) != 0 || session.Stop(context.Background()) != nil {
		t.Fatalf("err = %v runner calls = %d", err, len(runner.requests))
	}
}

func TestLlamaServer_StartsAndStopsChild(t *testing.T) {
	executable, model := installedFiles(t, "model.gguf")
	port := freePort(t)
	server := NewLlamaServer(NewProcessRunner(), LlamaServerSettings{Executable: executable, ModelFile: model, Port: port, HTTP: &http.Client{Timeout: time.Second}, Clock: NewSystemClock()})

	session, err := server.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	isHealthyWhileRunning := server.isHealthy(context.Background())
	stopErr := session.Stop(context.Background())
	secondStopErr := session.Stop(context.Background())

	if !isHealthyWhileRunning || stopErr != nil || secondStopErr != nil || server.isHealthy(context.Background()) {
		t.Fatalf("healthy = %v stop = %v %v", isHealthyWhileRunning, stopErr, secondStopErr)
	}
}

func TestLlamaServer_ChildFailsDuringStart(t *testing.T) {
	executable, model := installedFiles(t, "fail.gguf")
	server := NewLlamaServer(NewProcessRunner(), LlamaServerSettings{Executable: executable, ModelFile: model, Port: freePort(t), HTTP: &http.Client{Timeout: time.Second}, Clock: NewSystemClock()})

	started := time.Now()
	_, err := server.Start(context.Background())

	var processError ProcessError
	if !errors.As(err, &processError) || !strings.Contains(processError.StderrTail, "out of memory") || time.Since(started) > 5*time.Second {
		t.Fatalf("err = %v after %v", err, time.Since(started))
	}
}

func TestLlamaServer_HealthTimeout(t *testing.T) {
	loading := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer loading.Close()
	model := filepath.Join(t.TempDir(), "model.gguf")
	writeTestFile(t, model, "gguf")
	runner := &blockingRunner{}
	server := NewLlamaServer(runner, LlamaServerSettings{Executable: processHelperPath, ModelFile: model, Port: portOf(t, loading.URL), HTTP: loading.Client(), Clock: &firingClock{}})

	_, err := server.Start(context.Background())

	if !errors.Is(err, ErrLocalAdvisorStartTimeout) || !runner.wasCancelled() {
		t.Fatalf("err = %v cancelled = %v", err, runner.wasCancelled())
	}
}

type blockingRunner struct {
	cancelled atomic.Bool
}

func (runner *blockingRunner) Run(ctx context.Context, _ ProcessRequest) (ProcessResult, error) {
	<-ctx.Done()
	runner.cancelled.Store(true)
	return ProcessResult{}, ctx.Err()
}

func (runner *blockingRunner) wasCancelled() bool {
	return runner.cancelled.Load()
}

func TestLlamaServer_MissingFiles(t *testing.T) {
	executable, model := installedFiles(t, "model.gguf")
	missing := filepath.Join(t.TempDir(), "missing")
	cases := []struct {
		name       string
		executable string
		model      string
	}{
		{name: "MissingExecutable", executable: missing, model: model},
		{name: "MissingModel", executable: executable, model: missing},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := NewLlamaServer(&scriptedRunner{}, LlamaServerSettings{Executable: testCase.executable, ModelFile: testCase.model, Port: freePort(t), HTTP: &http.Client{Timeout: time.Second}, Clock: &firingClock{}})

			_, err := server.Start(context.Background())

			if !errors.Is(err, ErrLocalAdvisorNotInstalled) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestLlamaServer_Arguments(t *testing.T) {
	server := NewLlamaServer(&scriptedRunner{}, LlamaServerSettings{ModelFile: "m.gguf", Port: 8089})

	arguments := strings.Join(server.arguments(), " ")

	if arguments != "-m m.gguf -ngl 99 -c 8192 --port 8089 --host 127.0.0.1 --jinja -fa on --parallel 1" {
		t.Fatalf("arguments = %s", arguments)
	}
}

type advisorServerSpy struct {
	starts int
	stops  int
	err    error
}

func (spy *advisorServerSpy) Start(context.Context) (LlamaServerSession, error) {
	spy.starts++
	if spy.err != nil {
		return LlamaServerSession{}, spy.err
	}
	return LlamaServerSession{stop: func(context.Context) error {
		spy.stops++
		return nil
	}}, nil
}

type localAdvisorStub struct{ calls int }

func (stub *localAdvisorStub) SuggestCredits(context.Context, []services.CreditRequest) ([]services.CreditSuggestion, error) {
	stub.calls++
	return []services.CreditSuggestion{{Key: "k"}}, nil
}

func (stub *localAdvisorStub) SuggestGenres(context.Context, []services.GenreRequest) ([]services.GenreSuggestion, error) {
	stub.calls++
	return []services.GenreSuggestion{{Key: "k"}}, nil
}

func (stub *localAdvisorStub) Name() string { return "local:stub" }

func (stub *localAdvisorStub) ProbeAdvisor(context.Context) services.AdvisorReport {
	return services.AdvisorReport{Name: "local:stub", IsAvailable: true}
}

func TestLazyLlamaAdvisor_StartsOnceAndStopsOnClose(t *testing.T) {
	server := &advisorServerSpy{}
	inner := &localAdvisorStub{}
	advisor := NewLazyLlamaAdvisor(inner, server)

	closeBeforeStart := advisor.Close(context.Background())
	_, creditErr := advisor.SuggestCredits(context.Background(), nil)
	_, genreErr := advisor.SuggestGenres(context.Background(), nil)
	_, againErr := advisor.SuggestCredits(context.Background(), nil)
	closeErr := advisor.Close(context.Background())
	secondCloseErr := advisor.Close(context.Background())

	if closeBeforeStart != nil || creditErr != nil || genreErr != nil || againErr != nil || closeErr != nil || secondCloseErr != nil {
		t.Fatal("unexpected error")
	}
	if server.starts != 1 || server.stops != 1 || inner.calls != 3 || advisor.Name() != "local:stub" || !advisor.ProbeAdvisor(context.Background()).IsAvailable {
		t.Fatalf("starts = %d stops = %d calls = %d", server.starts, server.stops, inner.calls)
	}
}

func TestLazyLlamaAdvisor_StartFailure(t *testing.T) {
	server := &advisorServerSpy{err: ErrLocalAdvisorNotInstalled}
	inner := &localAdvisorStub{}
	advisor := NewLazyLlamaAdvisor(inner, server)

	_, creditErr := advisor.SuggestCredits(context.Background(), nil)
	_, genreErr := advisor.SuggestGenres(context.Background(), nil)

	if !errors.Is(creditErr, ErrLocalAdvisorNotInstalled) || !errors.Is(genreErr, ErrLocalAdvisorNotInstalled) || inner.calls != 0 || server.starts != 2 {
		t.Fatalf("errors = %v %v calls = %d starts = %d", creditErr, genreErr, inner.calls, server.starts)
	}
}
