package advisor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/UrielJaloto/music-ops/internal/services/crediting"
)

type silentClock struct{}

func (silentClock) After(time.Duration) <-chan time.Time { return make(chan time.Time) }

type cancellingClock struct{ cancel context.CancelFunc }

func (clock cancellingClock) After(time.Duration) <-chan time.Time {
	clock.cancel()
	return make(chan time.Time)
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestAdvisorCache_PutFails(t *testing.T) {
	errStore := errors.New("store down")
	cache := newMemoryByteCache()
	cache.putErr = errStore
	inner := &scriptedAdvisor{credits: map[string]crediting.CreditSuggestion{"a": goodCredit("a", "Song A", "Main")}}

	_, err := NewCreditAdvisorCache(inner, cache, "x").SuggestCredits(context.Background(), songRequests())

	if !errors.Is(err, errStore) {
		t.Fatalf("err = %v", err)
	}
}

func TestAdvisorCache_UnrequestedAnswerIsNotStored(t *testing.T) {
	cache := newMemoryByteCache()
	inner := &scriptedAdvisor{credits: map[string]crediting.CreditSuggestion{"a": goodCredit("stranger", "Song A", "Main")}}

	suggestions, err := NewCreditAdvisorCache(inner, cache, "x").SuggestCredits(context.Background(), songRequests())

	if err != nil || len(suggestions) != 1 || len(cache.values) != 0 {
		t.Fatalf("suggestions = %v cached = %v err = %v", suggestions, cache.values, err)
	}
}

func TestLlamaAdvisor_InvalidBaseURL(t *testing.T) {
	advisor := NewLlamaAdvisor(LlamaAdvisorSettings{BaseURL: "http://\x7f", Model: "m", Clock: &firingClock{}}, http.DefaultClient)

	report := advisor.ProbeAdvisor(context.Background())
	_, err := advisor.SuggestCredits(context.Background(), creditRequests(1))

	if report.IsAvailable || report.Detail == "" || err == nil || !strings.Contains(err.Error(), "building advisor request") {
		t.Fatalf("report = %+v err = %v", report, err)
	}
}

func TestLlamaAdvisor_CancelledWhileLoading(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	advisor, server := newLlamaTestAdvisor(t, func(chatRequest, int) llamaReply { return llamaReply{status: 503} })
	advisor.settings.Clock = cancellingClock{cancel: cancel}

	_, err := advisor.SuggestCredits(ctx, creditRequests(1))

	if !errors.Is(err, context.Canceled) || len(server.requests) != 1 {
		t.Fatalf("err = %v calls = %d", err, len(server.requests))
	}
}

func numberTask(build func([]int) ([]byte, map[string]string, error)) advisorTask[int, int] {
	return advisorTask[int, int]{build: build, decode: func([]byte, map[string]string) ([]int, error) { return []int{1}, nil }}
}

func buildNumbers([]int) ([]byte, map[string]string, error) { return []byte("x"), nil, nil }

func scriptedCompletions(answers ...error) completeFunc {
	call := 0
	return func(context.Context, chatRequest) (completion, error) {
		call++
		if call == 1 {
			return completion{isTruncated: true}, nil
		}
		return completion{content: "x"}, answers[call-2]
	}
}

func TestSuggestInBatches_Failures(t *testing.T) {
	errBuild := errors.New("build")
	errComplete := errors.New("complete")
	cases := []struct {
		name     string
		task     advisorTask[int, int]
		complete completeFunc
		expected error
	}{
		{name: "BuildFails", task: numberTask(func([]int) ([]byte, map[string]string, error) { return nil, nil, errBuild }), complete: scriptedCompletions(), expected: errBuild},
		{name: "FirstHalfFails", task: numberTask(buildNumbers), complete: scriptedCompletions(errComplete), expected: errComplete},
		{name: "SecondHalfFails", task: numberTask(buildNumbers), complete: scriptedCompletions(nil, errComplete), expected: errComplete},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			suggestions, err := suggestInBatches(context.Background(), testCase.complete, testCase.task, []int{1, 2})

			if !errors.Is(err, testCase.expected) || suggestions != nil {
				t.Fatalf("suggestions = %v err = %v", suggestions, err)
			}
		})
	}
}

func TestDecodeChatResponse_Failures(t *testing.T) {
	errRead := errors.New("read")
	unreadable := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(iotest.ErrReader(errRead))}
	empty := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"choices":[]}`))}

	_, readErr := decodeChatResponse(unreadable)
	_, emptyErr := decodeChatResponse(empty)

	if !errors.Is(readErr, errRead) || !errors.Is(emptyErr, ErrAdvisorAnswerMalformed) {
		t.Fatalf("errors = %v %v", readErr, emptyErr)
	}
}

func TestLlamaServer_IsHealthy_InvalidPort(t *testing.T) {
	server := NewLlamaServer(nil, LlamaServerSettings{Port: -1, HTTP: http.DefaultClient})

	if server.isHealthy(context.Background()) {
		t.Fatal("healthy")
	}
}

func TestLlamaServer_WaitUntilHealthy_Cancelled(t *testing.T) {
	server := NewLlamaServer(nil, LlamaServerSettings{Clock: silentClock{}})

	err := server.waitUntilHealthy(cancelledContext(), &childProcess{done: make(chan struct{})})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestAwaitExit_ChildNeverExits(t *testing.T) {
	err := awaitExit(cancelledContext(), &childProcess{done: make(chan struct{})})

	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "stopping local advisor") {
		t.Fatalf("err = %v", err)
	}
}
