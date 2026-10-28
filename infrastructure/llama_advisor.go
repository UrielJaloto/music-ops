package infrastructure

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/UrielJaloto/music-ops/services"
)

const (
	llamaTokensPerItem    = 64
	llamaTokensOverhead   = 256
	llamaFinishLength     = "length"
	llamaLoadingInterval  = time.Second
	llamaMalformedRetries = 1
	llamaResponseLimit    = 8 << 20
	localAdvisorPrefix    = "local:"
	chatCompletionsPath   = "/v1/chat/completions"
	healthPath            = "/health"
)

var (
	ErrAdvisorAnswerTooLong = errors.New("advisor answer was cut by the token limit")
	ErrAdvisorUnavailable   = errors.New("advisor is unavailable")
	errAdvisorLoading       = errors.New("advisor model is loading")
)

type LlamaAdvisorSettings struct {
	BaseURL        string
	Model          string
	LoadingTimeout time.Duration
	Clock          StabilityClock
}

type LlamaAdvisor struct {
	settings LlamaAdvisorSettings
	http     *http.Client
}

func NewLlamaAdvisor(settings LlamaAdvisorSettings, http *http.Client) LlamaAdvisor {
	return LlamaAdvisor{settings: settings, http: http}
}

func (advisor LlamaAdvisor) Name() string {
	return localAdvisorPrefix + advisor.settings.Model
}

func (advisor LlamaAdvisor) SuggestCredits(ctx context.Context, requests []services.CreditRequest) ([]services.CreditSuggestion, error) {
	task := advisorTask[services.CreditRequest, services.CreditSuggestion]{
		system: CreditSystemPrompt(), schemaName: creditSchemaName, schema: CreditSchema(),
		build: BuildCreditUserMessage, decode: DecodeCreditSuggestions,
	}
	return suggestInBatches(ctx, advisor.complete, task, requests)
}

func (advisor LlamaAdvisor) SuggestGenres(ctx context.Context, requests []services.GenreRequest) ([]services.GenreSuggestion, error) {
	task := advisorTask[services.GenreRequest, services.GenreSuggestion]{
		system: GenreSystemPrompt(), schemaName: genreSchemaName, schema: GenreSchema(),
		build: BuildGenreUserMessage, decode: DecodeGenreSuggestions,
	}
	return suggestInBatches(ctx, advisor.complete, task, requests)
}

func (advisor LlamaAdvisor) ProbeAdvisor(ctx context.Context) services.AdvisorReport {
	report := services.AdvisorReport{Name: advisor.Name()}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, advisor.settings.BaseURL+healthPath, nil)
	if err != nil {
		report.Detail = err.Error()
		return report
	}
	response, err := advisor.http.Do(request)
	if err != nil {
		report.Detail = err.Error()
		return report
	}
	defer func() { _ = response.Body.Close() }()
	report.IsAvailable = response.StatusCode == http.StatusOK
	report.Detail = response.Status
	return report
}

type advisorTask[R, S any] struct {
	system     string
	schemaName string
	schema     map[string]any
	build      func([]R) ([]byte, map[string]string, error)
	decode     func([]byte, map[string]string) ([]S, error)
}

type completion struct {
	content     string
	isTruncated bool
}

type completeFunc func(ctx context.Context, request chatRequest) (completion, error)

func suggestInBatches[R, S any](ctx context.Context, complete completeFunc, task advisorTask[R, S], requests []R) ([]S, error) {
	if len(requests) == 0 {
		return []S{}, nil
	}
	message, keys, err := task.build(requests)
	if err != nil {
		return nil, fmt.Errorf("building advisor message: %w", err)
	}
	request := newChatRequest(task.system, string(message), task.schemaName, task.schema, len(requests))
	suggestions, err := completeAndDecode(ctx, complete, request, func(raw []byte) ([]S, error) { return task.decode(raw, keys) })
	if !errors.Is(err, ErrAdvisorAnswerTooLong) || len(requests) == 1 {
		return suggestions, err
	}
	return splitInHalves(ctx, complete, task, requests)
}

func splitInHalves[R, S any](ctx context.Context, complete completeFunc, task advisorTask[R, S], requests []R) ([]S, error) {
	middle := len(requests) / 2
	first, err := suggestInBatches(ctx, complete, task, requests[:middle])
	if err != nil {
		return nil, err
	}
	second, err := suggestInBatches(ctx, complete, task, requests[middle:])
	if err != nil {
		return nil, err
	}
	return append(first, second...), nil
}

func completeAndDecode[S any](ctx context.Context, complete completeFunc, request chatRequest, decode func([]byte) ([]S, error)) ([]S, error) {
	var lastErr error
	for range llamaMalformedRetries + 1 {
		answer, err := complete(ctx, request)
		if err != nil {
			return nil, err
		}
		if answer.isTruncated {
			return nil, ErrAdvisorAnswerTooLong
		}
		suggestions, err := decode([]byte(answer.content))
		if !errors.Is(err, ErrAdvisorAnswerMalformed) {
			return suggestions, err
		}
		lastErr = err
	}
	return nil, lastErr
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatJSONSchema struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

type chatResponseFormat struct {
	Type       string         `json:"type"`
	JSONSchema chatJSONSchema `json:"json_schema"`
}

type chatRequest struct {
	Model              string             `json:"model"`
	Messages           []chatMessage      `json:"messages"`
	Temperature        float64            `json:"temperature"`
	MaxTokens          int                `json:"max_tokens"`
	ResponseFormat     chatResponseFormat `json:"response_format"`
	ChatTemplateKwargs map[string]any     `json:"chat_template_kwargs,omitempty"`
}

func newChatRequest(system, user, schemaName string, schema map[string]any, items int) chatRequest {
	return chatRequest{
		Messages:       []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}},
		MaxTokens:      llamaTokensPerItem*items + llamaTokensOverhead,
		ResponseFormat: chatResponseFormat{Type: "json_schema", JSONSchema: chatJSONSchema{Name: schemaName, Strict: true, Schema: schema}},
	}
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

func (advisor LlamaAdvisor) complete(ctx context.Context, request chatRequest) (completion, error) {
	request.Model = advisor.settings.Model
	request.ChatTemplateKwargs = map[string]any{"enable_thinking": false}
	body, err := json.Marshal(request)
	if err != nil {
		return completion{}, fmt.Errorf("encoding advisor request: %w", err)
	}
	attempts := max(int(advisor.settings.LoadingTimeout/llamaLoadingInterval), 1)
	for attempt := range attempts {
		answer, err := advisor.post(ctx, body)
		if !errors.Is(err, errAdvisorLoading) || attempt == attempts-1 {
			return answer, err
		}
		if err := advisor.waitWhileLoading(ctx); err != nil {
			return completion{}, err
		}
	}
	return completion{}, errAdvisorLoading
}

func (advisor LlamaAdvisor) waitWhileLoading(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-advisor.settings.Clock.After(llamaLoadingInterval):
		return nil
	}
}

func (advisor LlamaAdvisor) post(ctx context.Context, body []byte) (completion, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, advisor.settings.BaseURL+chatCompletionsPath, bytes.NewReader(body))
	if err != nil {
		return completion{}, fmt.Errorf("building advisor request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := advisor.http.Do(request)
	if err != nil {
		return completion{}, fmt.Errorf("%w: %w", ErrAdvisorUnavailable, err)
	}
	defer func() { _ = response.Body.Close() }()
	return decodeChatResponse(response)
}

func decodeChatResponse(response *http.Response) (completion, error) {
	raw, err := io.ReadAll(io.LimitReader(response.Body, llamaResponseLimit))
	if err != nil {
		return completion{}, fmt.Errorf("reading advisor response: %w", err)
	}
	if response.StatusCode == http.StatusServiceUnavailable {
		return completion{}, errAdvisorLoading
	}
	if response.StatusCode != http.StatusOK {
		return completion{}, fmt.Errorf("%w: status %d: %s", ErrAdvisorUnavailable, response.StatusCode, raw)
	}
	var decoded chatResponse
	if err := json.Unmarshal(raw, &decoded); err != nil || len(decoded.Choices) == 0 {
		return completion{}, fmt.Errorf("%w: unexpected completion body", ErrAdvisorAnswerMalformed)
	}
	choice := decoded.Choices[0]
	return completion{content: choice.Message.Content, isTruncated: choice.FinishReason == llamaFinishLength}, nil
}
