package infrastructure

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/time/rate"

	"github.com/UrielJaloto/music-ops/services"
)

type lastFMServer struct {
	mutex     sync.Mutex
	responses []lastFMReply
	queries   []url.Values
	agents    []string
}

type lastFMReply struct {
	status int
	body   string
}

func (server *lastFMServer) handler(writer http.ResponseWriter, request *http.Request) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.queries = append(server.queries, request.URL.Query())
	server.agents = append(server.agents, request.UserAgent())
	reply := server.responses[min(len(server.queries)-1, len(server.responses)-1)]
	writer.WriteHeader(reply.status)
	_, _ = writer.Write([]byte(reply.body))
}

type immediateBackoff struct{ waits []int }

func (backoff *immediateBackoff) Wait(_ context.Context, attempt int) error {
	backoff.waits = append(backoff.waits, attempt)
	return nil
}

func newLastFMTestClient(t *testing.T, replies ...lastFMReply) (LastFMClient, *lastFMServer, *immediateBackoff) {
	t.Helper()
	fake := &lastFMServer{responses: replies}
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(server.Close)
	backoff := &immediateBackoff{}
	client := NewLastFMClient(LastFMSettings{APIKey: "key", BaseURL: server.URL + "/2.0/", Version: "1.2.3"}, server.Client(), rate.NewLimiter(rate.Inf, 1), backoff)
	return client, fake, backoff
}

const queenTags = `{"toptags":{"tag":[{"name":"Rock"},{"name":"classic rock "},{"name":"1975"},{"name":"seen live"},{"name":"Queen"},{"name":"rock"},{"name":"British"},{"name":"glam rock"},{"name":"hard rock"},{"name":"80s"}]}}`

func TestLastFMClient_ArtistTags_HappyPathAndNoiseFiltering(t *testing.T) {
	client, server, _ := newLastFMTestClient(t, lastFMReply{status: 200, body: queenTags})

	tags, err := client.ArtistTags(context.Background(), "Queen")

	expected := []string{"rock", "classic rock", "british", "glam rock", "hard rock"}
	if diff := cmp.Diff(expected, tags); diff != "" || err != nil {
		t.Fatalf("tags (-want +got):\n%s err = %v", diff, err)
	}
	query := server.queries[0]
	if query.Get("method") != "artist.gettoptags" || query.Get("artist") != "Queen" || query.Get("api_key") != "key" || query.Get("format") != "json" || query.Get("autocorrect") != "1" || server.agents[0] != "music-ops/1.2.3" {
		t.Fatalf("query = %v agent = %s", query, server.agents[0])
	}
}

func TestLastFMClient_TrackTags_EncodesNames(t *testing.T) {
	cases := []struct{ artist, title string }{
		{artist: "AC/DC", title: "T.N.T."},
		{artist: "¥$", title: "Carnival"},
		{artist: "Bênção", title: "Canção & Fé"},
	}
	for _, testCase := range cases {
		t.Run(testCase.artist, func(t *testing.T) {
			client, server, _ := newLastFMTestClient(t, lastFMReply{status: 200, body: `{"toptags":{"tag":[{"name":"rock"}]}}`})

			tags, err := client.TrackTags(context.Background(), testCase.artist, testCase.title)

			query := server.queries[0]
			if err != nil || len(tags) != 1 || query.Get("method") != "track.gettoptags" || query.Get("artist") != testCase.artist || query.Get("track") != testCase.title {
				t.Fatalf("query = %v tags = %v err = %v", query, tags, err)
			}
		})
	}
}

func TestLastFMClient_Outcomes(t *testing.T) {
	cases := []struct {
		name          string
		replies       []lastFMReply
		expectedTags  []string
		expectedError error
		expectedCalls int
	}{
		{name: "NotFound_EmptyList", replies: []lastFMReply{{status: 200, body: `{"error":6,"message":"Track not found"}`}}, expectedTags: []string{}, expectedCalls: 1},
		{name: "NoTags_EmptyList", replies: []lastFMReply{{status: 200, body: `{"toptags":{"tag":[]}}`}}, expectedTags: []string{}, expectedCalls: 1},
		{name: "ServerErrorThenSuccess", replies: []lastFMReply{{status: 503, body: ""}, {status: 200, body: `{"toptags":{"tag":[{"name":"pop"}]}}`}}, expectedTags: []string{"pop"}, expectedCalls: 2},
		{name: "RateLimitedThenSuccess", replies: []lastFMReply{{status: 429, body: ""}, {status: 200, body: `{"toptags":{"tag":[{"name":"pop"}]}}`}}, expectedTags: []string{"pop"}, expectedCalls: 2},
		{name: "RetryableCodeThenSuccess", replies: []lastFMReply{{status: 200, body: `{"error":29,"message":"Rate limit"}`}, {status: 200, body: `{"toptags":{"tag":[{"name":"pop"}]}}`}}, expectedTags: []string{"pop"}, expectedCalls: 2},
		{name: "RetriesExhausted", replies: []lastFMReply{{status: 500, body: ""}}, expectedError: errLastFMRetryable, expectedCalls: 5},
		{name: "InvalidKey", replies: []lastFMReply{{status: 403, body: `{"error":10,"message":"Invalid API key"}`}}, expectedError: services.ErrLastFMKeyInvalid, expectedCalls: 1},
		{name: "SuspendedKey", replies: []lastFMReply{{status: 200, body: `{"error":26,"message":"Suspended"}`}}, expectedError: services.ErrLastFMKeyInvalid, expectedCalls: 1},
		{name: "OtherClientError_NoRetry", replies: []lastFMReply{{status: 400, body: `{"error":7,"message":"Invalid resource"}`}}, expectedError: errLastFMRejected, expectedCalls: 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			client, server, backoff := newLastFMTestClient(t, testCase.replies...)

			tags, err := client.TrackTags(context.Background(), "Artist", "Song")

			if !errors.Is(err, testCase.expectedError) || len(server.queries) != testCase.expectedCalls || len(backoff.waits) != testCase.expectedCalls-1 {
				t.Fatalf("err = %v calls = %d waits = %v", err, len(server.queries), backoff.waits)
			}
			if diff := cmp.Diff(testCase.expectedTags, tags); testCase.expectedError == nil && diff != "" {
				t.Fatalf("tags (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLastFMClient_InvalidKeyStopsFurtherCalls(t *testing.T) {
	client, server, _ := newLastFMTestClient(t, lastFMReply{status: 403, body: `{"error":10,"message":"Invalid API key"}`})

	_, firstErr := client.ArtistTags(context.Background(), "A")
	_, secondErr := client.TrackTags(context.Background(), "B", "C")

	if !errors.Is(firstErr, services.ErrLastFMKeyInvalid) || !errors.Is(secondErr, services.ErrLastFMKeyInvalid) || len(server.queries) != 1 {
		t.Fatalf("errors = %v %v calls = %d", firstErr, secondErr, len(server.queries))
	}
}

func TestLastFMClient_MalformedJSON(t *testing.T) {
	client, _, _ := newLastFMTestClient(t, lastFMReply{status: 200, body: `{"toptags":`})

	_, err := client.ArtistTags(context.Background(), "A")

	if err == nil || errors.Is(err, errLastFMRetryable) {
		t.Fatalf("err = %v", err)
	}
}

func TestLastFMClient_NetworkErrorIsRetried(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	address := server.URL
	server.Close()
	backoff := &immediateBackoff{}
	client := NewLastFMClient(LastFMSettings{BaseURL: address + "/"}, http.DefaultClient, rate.NewLimiter(rate.Inf, 1), backoff)

	_, err := client.ArtistTags(context.Background(), "A")

	if !errors.Is(err, errLastFMRetryable) || len(backoff.waits) != 4 {
		t.Fatalf("err = %v waits = %v", err, backoff.waits)
	}
}

func TestLastFMClient_LimiterTakesTokenPerAttempt(t *testing.T) {
	fake := &lastFMServer{responses: []lastFMReply{{status: 503}, {status: 200, body: `{"toptags":{"tag":[]}}`}}}
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer server.Close()
	limiter := rate.NewLimiter(rate.Every(time.Hour), 2)
	client := NewLastFMClient(LastFMSettings{BaseURL: server.URL + "/"}, server.Client(), limiter, &immediateBackoff{})

	_, err := client.ArtistTags(context.Background(), "A")

	reservation := limiter.ReserveN(time.Now(), 1)
	defer reservation.Cancel()
	if err != nil || reservation.Delay() < 59*time.Minute {
		t.Fatalf("err = %v next token in %v", err, reservation.Delay())
	}
}

func TestLastFMClient_LimiterCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := NewLastFMClient(LastFMSettings{BaseURL: "http://127.0.0.1:1/"}, http.DefaultClient, rate.NewLimiter(rate.Every(time.Hour), 1), &immediateBackoff{})

	_, err := client.ArtistTags(ctx, "A")

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

type blockingClock struct{}

func (blockingClock) After(time.Duration) <-chan time.Time { return make(chan time.Time) }

func TestLastFMClient_CancellationDuringBackoff(t *testing.T) {
	fake := &lastFMServer{responses: []lastFMReply{{status: 503}}}
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	backoff := NewExponentialBackoff(func() float64 { return 0.5 }, blockingClock{})
	client := NewLastFMClient(LastFMSettings{BaseURL: server.URL + "/"}, server.Client(), rate.NewLimiter(rate.Inf, 1), cancellingBackoff{inner: backoff, cancel: cancel})

	_, err := client.ArtistTags(ctx, "A")

	if !errors.Is(err, context.Canceled) || len(fake.queries) != 1 {
		t.Fatalf("err = %v calls = %d", err, len(fake.queries))
	}
}

type cancellingBackoff struct {
	inner  Backoff
	cancel context.CancelFunc
}

func (backoff cancellingBackoff) Wait(ctx context.Context, attempt int) error {
	backoff.cancel()
	return backoff.inner.Wait(ctx, attempt)
}

func TestExponentialBackoff_Delay(t *testing.T) {
	cases := []struct {
		name     string
		random   float64
		attempt  int
		expected time.Duration
	}{
		{name: "FirstNoJitter", random: 0.5, attempt: 0, expected: 500 * time.Millisecond},
		{name: "SecondNoJitter", random: 0.5, attempt: 1, expected: time.Second},
		{name: "FourthNoJitter", random: 0.5, attempt: 3, expected: 4 * time.Second},
		{name: "MinimumJitter", random: 0, attempt: 0, expected: 400 * time.Millisecond},
		{name: "MaximumJitter", random: 1, attempt: 0, expected: 600 * time.Millisecond},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			delay := NewExponentialBackoff(func() float64 { return testCase.random }, blockingClock{}).Delay(testCase.attempt)

			if (delay - testCase.expected).Abs() > time.Microsecond {
				t.Fatalf("delay = %v", delay)
			}
		})
	}
}

type firingClock struct{ requested []time.Duration }

func (clock *firingClock) After(duration time.Duration) <-chan time.Time {
	clock.requested = append(clock.requested, duration)
	fired := make(chan time.Time, 1)
	fired <- time.Time{}
	return fired
}

func TestExponentialBackoff_WaitUsesDelay(t *testing.T) {
	clock := &firingClock{}

	err := NewExponentialBackoff(func() float64 { return 0.5 }, clock).Wait(context.Background(), 2)

	if err != nil || len(clock.requested) != 1 || clock.requested[0] != 2*time.Second {
		t.Fatalf("err = %v requested = %v", err, clock.requested)
	}
}

func TestLastFMClient_ProbeTagSource(t *testing.T) {
	client, server, _ := newLastFMTestClient(t, lastFMReply{status: 200, body: queenTags})
	failing, _, _ := newLastFMTestClient(t, lastFMReply{status: 403, body: `{"error":10}`})

	err := client.ProbeTagSource(context.Background())
	failingErr := failing.ProbeTagSource(context.Background())

	if err != nil || server.queries[0].Get("artist") != "Queen" || !errors.Is(failingErr, services.ErrLastFMKeyInvalid) {
		t.Fatalf("err = %v failing = %v", err, failingErr)
	}
}
