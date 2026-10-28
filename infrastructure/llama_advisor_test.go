package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
)

type llamaReply struct {
	status  int
	content string
	finish  string
}

type fakeLlamaServer struct {
	mutex    sync.Mutex
	reply    func(request chatRequest, call int) llamaReply
	requests []chatRequest
	bodies   []map[string]any
}

func (server *fakeLlamaServer) handler(writer http.ResponseWriter, request *http.Request) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	if request.URL.Path == healthPath {
		writer.WriteHeader(http.StatusOK)
		return
	}
	var decoded chatRequest
	var generic map[string]any
	body, _ := io.ReadAll(request.Body)
	_ = json.Unmarshal(body, &decoded)
	_ = json.Unmarshal(body, &generic)
	server.requests = append(server.requests, decoded)
	server.bodies = append(server.bodies, generic)
	reply := server.reply(decoded, len(server.requests))
	writer.WriteHeader(reply.status)
	answer, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"message": map[string]any{"content": reply.content}, "finish_reason": reply.finish}}})
	_, _ = writer.Write(answer)
}

func newLlamaTestAdvisor(t *testing.T, reply func(request chatRequest, call int) llamaReply) (LlamaAdvisor, *fakeLlamaServer) {
	t.Helper()
	fake := &fakeLlamaServer{reply: reply}
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(server.Close)
	settings := LlamaAdvisorSettings{BaseURL: server.URL, Model: "qwen3.5-9b", LoadingTimeout: 60 * time.Second, Clock: &firingClock{}}
	return NewLlamaAdvisor(settings, server.Client()), fake
}

func userItems(t *testing.T, request chatRequest) []map[string]any {
	t.Helper()
	var message struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(request.Messages[1].Content), &message); err != nil {
		t.Fatal(err)
	}
	return message.Items
}

func echoCredits(t *testing.T) func(request chatRequest, call int) llamaReply {
	return func(request chatRequest, _ int) llamaReply {
		items := []map[string]any{}
		for _, item := range userItems(t, request) {
			items = append(items, map[string]any{"key": item["key"], "title": "Clean " + item["title"].(string), "artists": []string{item["artist"].(string)}})
		}
		content, _ := json.Marshal(map[string]any{"items": items})
		return llamaReply{status: 200, content: string(content), finish: "stop"}
	}
}

func creditRequests(count int) []services.CreditRequest {
	requests := make([]services.CreditRequest, 0, count)
	for index := range count {
		requests = append(requests, services.CreditRequest{Key: fmt.Sprintf("id-%d", index), Source: domain.CreditSource{Title: fmt.Sprintf("Song %d", index), Artist: "Artist"}})
	}
	return requests
}

func TestLlamaAdvisor_RequestBody(t *testing.T) {
	advisor, server := newLlamaTestAdvisor(t, echoCredits(t))

	_, err := advisor.SuggestCredits(context.Background(), creditRequests(3))

	body := server.bodies[0]
	format := body["response_format"].(map[string]any)
	schema := format["json_schema"].(map[string]any)
	if err != nil || body["model"] != "qwen3.5-9b" || body["temperature"] != float64(0) || body["max_tokens"] != float64(64*3+256) {
		t.Fatalf("body = %v err = %v", body, err)
	}
	if format["type"] != "json_schema" || schema["name"] != "credit_suggestions" || schema["strict"] != true || schema["schema"] == nil {
		t.Fatalf("response format = %v", format)
	}
	if diff := cmp.Diff(map[string]any{"enable_thinking": false}, body["chat_template_kwargs"]); diff != "" {
		t.Fatalf("kwargs (-want +got):\n%s", diff)
	}
	messages := body["messages"].([]any)
	if messages[0].(map[string]any)["role"] != "system" || messages[0].(map[string]any)["content"] != CreditSystemPrompt() || messages[1].(map[string]any)["role"] != "user" {
		t.Fatalf("messages = %v", messages)
	}
}

func TestLlamaAdvisor_SuggestCredits_RemapsKeys(t *testing.T) {
	advisor, _ := newLlamaTestAdvisor(t, echoCredits(t))

	suggestions, err := advisor.SuggestCredits(context.Background(), creditRequests(2))

	expected := []services.CreditSuggestion{{Key: "id-0", Title: "Clean Song 0", Artists: []string{"Artist"}}, {Key: "id-1", Title: "Clean Song 1", Artists: []string{"Artist"}}}
	if diff := cmp.Diff(expected, suggestions); diff != "" || err != nil {
		t.Fatalf("suggestions (-want +got):\n%s err = %v", diff, err)
	}
}

func TestLlamaAdvisor_SuggestGenres(t *testing.T) {
	advisor, server := newLlamaTestAdvisor(t, func(request chatRequest, _ int) llamaReply {
		key := userItems(t, request)[0]["key"]
		return llamaReply{status: 200, content: fmt.Sprintf(`{"items":[{"key":%q,"genre":"Rock","subgenre":"Metal","is_brazilian":false,"is_known":true}]}`, key), finish: "stop"}
	})

	suggestions, err := advisor.SuggestGenres(context.Background(), []services.GenreRequest{{Key: "song-1", Title: "Iron Man", Artists: []string{"Black Sabbath"}, Tags: []string{"metal"}}})

	expected := []services.GenreSuggestion{{Key: "song-1", Genre: "Rock", Subgenre: "Metal", IsKnown: true}}
	if diff := cmp.Diff(expected, suggestions); diff != "" || err != nil || server.requests[0].ResponseFormat.JSONSchema.Name != "genre_suggestions" {
		t.Fatalf("suggestions (-want +got):\n%s err = %v", diff, err)
	}
}

func TestLlamaAdvisor_LengthSplitsBatch(t *testing.T) {
	echo := echoCredits(t)
	advisor, server := newLlamaTestAdvisor(t, func(request chatRequest, call int) llamaReply {
		if len(userItems(t, request)) > 1 {
			return llamaReply{status: 200, content: `{"items":[`, finish: "length"}
		}
		return echo(request, call)
	})

	suggestions, err := advisor.SuggestCredits(context.Background(), creditRequests(3))

	if err != nil || len(suggestions) != 3 || suggestions[2].Key != "id-2" || len(server.requests) != 5 {
		t.Fatalf("suggestions = %v calls = %d err = %v", suggestions, len(server.requests), err)
	}
}

func TestLlamaAdvisor_SingleItemTooLong(t *testing.T) {
	advisor, _ := newLlamaTestAdvisor(t, func(chatRequest, int) llamaReply {
		return llamaReply{status: 200, content: `{"items":[`, finish: "length"}
	})

	_, err := advisor.SuggestCredits(context.Background(), creditRequests(1))

	if !errors.Is(err, ErrAdvisorAnswerTooLong) {
		t.Fatalf("err = %v", err)
	}
}

func TestLlamaAdvisor_MalformedAnswer(t *testing.T) {
	cases := []struct {
		name          string
		goodFromCall  int
		expectedCalls int
		expectedError error
	}{
		{name: "MalformedThenValid_Retried", goodFromCall: 2, expectedCalls: 2},
		{name: "MalformedTwice_Error", goodFromCall: 99, expectedCalls: 2, expectedError: ErrAdvisorAnswerMalformed},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			echo := echoCredits(t)
			advisor, server := newLlamaTestAdvisor(t, func(request chatRequest, call int) llamaReply {
				if call < testCase.goodFromCall {
					return llamaReply{status: 200, content: "Sure, here you go", finish: "stop"}
				}
				return echo(request, call)
			})

			_, err := advisor.SuggestCredits(context.Background(), creditRequests(1))

			if !errors.Is(err, testCase.expectedError) || len(server.requests) != testCase.expectedCalls {
				t.Fatalf("err = %v calls = %d", err, len(server.requests))
			}
		})
	}
}

func TestLlamaAdvisor_ModelLoading(t *testing.T) {
	cases := []struct {
		name          string
		loadingCalls  int
		timeout       time.Duration
		expectedCalls int
		expectedError error
	}{
		{name: "LoadingThenReady", loadingCalls: 2, timeout: 60 * time.Second, expectedCalls: 3},
		{name: "LoadingPastTimeout", loadingCalls: 99, timeout: 3 * time.Second, expectedCalls: 3, expectedError: errAdvisorLoading},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			echo := echoCredits(t)
			advisor, server := newLlamaTestAdvisor(t, func(request chatRequest, call int) llamaReply {
				if call <= testCase.loadingCalls {
					return llamaReply{status: 503}
				}
				return echo(request, call)
			})
			advisor.settings.LoadingTimeout = testCase.timeout

			_, err := advisor.SuggestCredits(context.Background(), creditRequests(1))

			if !errors.Is(err, testCase.expectedError) || len(server.requests) != testCase.expectedCalls {
				t.Fatalf("err = %v calls = %d", err, len(server.requests))
			}
		})
	}
}

func TestLlamaAdvisor_ServerErrors(t *testing.T) {
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()
	stopped := NewLlamaAdvisor(LlamaAdvisorSettings{BaseURL: down.URL, Model: "m", Clock: &firingClock{}}, http.DefaultClient)
	failing, _ := newLlamaTestAdvisor(t, func(chatRequest, int) llamaReply { return llamaReply{status: 500} })

	_, downErr := stopped.SuggestCredits(context.Background(), creditRequests(1))
	_, failingErr := failing.SuggestGenres(context.Background(), []services.GenreRequest{{Key: "k"}})

	if !errors.Is(downErr, ErrAdvisorUnavailable) || !errors.Is(failingErr, ErrAdvisorUnavailable) {
		t.Fatalf("errors = %v %v", downErr, failingErr)
	}
}

func TestLlamaAdvisor_EmptyRequests(t *testing.T) {
	advisor, server := newLlamaTestAdvisor(t, echoCredits(t))

	suggestions, err := advisor.SuggestCredits(context.Background(), nil)

	if err != nil || len(suggestions) != 0 || len(server.requests) != 0 {
		t.Fatalf("suggestions = %v err = %v", suggestions, err)
	}
}

func TestLlamaAdvisor_ProbeAdvisor(t *testing.T) {
	advisor, _ := newLlamaTestAdvisor(t, echoCredits(t))
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()
	stopped := NewLlamaAdvisor(LlamaAdvisorSettings{BaseURL: down.URL, Model: "m"}, http.DefaultClient)

	up := advisor.ProbeAdvisor(context.Background())
	unavailable := stopped.ProbeAdvisor(context.Background())

	if !up.IsAvailable || up.Name != "local:qwen3.5-9b" || unavailable.IsAvailable || unavailable.Detail == "" || unavailable.Name != "local:m" {
		t.Fatalf("up = %+v down = %+v", up, unavailable)
	}
}
