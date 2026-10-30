package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/UrielJaloto/music-ops/services"
)

const (
	lastFMMethodTrackTags   = "track.gettoptags"
	lastFMMethodArtistTags  = "artist.gettoptags"
	lastFMMaximumAttempts   = 5
	lastFMKeptTags          = 5
	lastFMProbeArtist       = "Queen"
	lastFMNotFound          = 6
	lastFMBaseDelay         = 500 * time.Millisecond
	lastFMJitter            = 0.2
	lastFMResponseLimit     = 1 << 20
	lastFMRequestsPerSecond = 4
	userAgentPrefix         = "music-ops/"
)

var (
	errLastFMRetryable = errors.New("last.fm temporary failure")
	errLastFMRejected  = errors.New("last.fm rejected the request")
)

var (
	lastFMRetryableCodes  = []int{8, 11, 16, 29}
	lastFMKeyInvalidCodes = []int{10, 26}
	lastFMNoiseTags       = []string{"seen live", "favorites", "favourite", "my favorite", "love", "awesome", "spotify", "under 2000 listeners"}
	yearTagPattern        = regexp.MustCompile(`^[0-9]{4}$`)
)

type LastFMSettings struct {
	APIKey  string
	BaseURL string
	Version string
}

type Backoff interface {
	Wait(ctx context.Context, attempt int) error
}

type LastFMClient struct {
	settings     LastFMSettings
	http         *http.Client
	limiter      *rate.Limiter
	backoff      Backoff
	isKeyInvalid *atomic.Bool
}

func NewLastFMClient(settings LastFMSettings, http *http.Client, limiter *rate.Limiter, backoff Backoff) LastFMClient {
	return LastFMClient{settings: settings, http: http, limiter: limiter, backoff: backoff, isKeyInvalid: &atomic.Bool{}}
}

func (client LastFMClient) TrackTags(ctx context.Context, artist, title string) ([]string, error) {
	return client.topTags(ctx, url.Values{"method": {lastFMMethodTrackTags}, "artist": {artist}, "track": {title}}, artist)
}

func (client LastFMClient) ArtistTags(ctx context.Context, artist string) ([]string, error) {
	return client.topTags(ctx, url.Values{"method": {lastFMMethodArtistTags}, "artist": {artist}}, artist)
}

func (client LastFMClient) ProbeTagSource(ctx context.Context) error {
	_, err := client.ArtistTags(ctx, lastFMProbeArtist)
	return err
}

func (client LastFMClient) topTags(ctx context.Context, query url.Values, artist string) ([]string, error) {
	if client.isKeyInvalid.Load() {
		return nil, services.ErrLastFMKeyInvalid
	}
	query.Set("api_key", client.settings.APIKey)
	query.Set("format", "json")
	query.Set("autocorrect", "1")
	response, err := client.fetchWithRetries(ctx, client.settings.BaseURL+"?"+query.Encode())
	if errors.Is(err, services.ErrLastFMKeyInvalid) {
		client.isKeyInvalid.Store(true)
	}
	if err != nil {
		return nil, err
	}
	return filterTags(response.tagNames(), artist), nil
}

func (client LastFMClient) fetchWithRetries(ctx context.Context, address string) (lastFMResponse, error) {
	var lastErr error
	for attempt := range lastFMMaximumAttempts {
		if attempt > 0 {
			if err := client.backoff.Wait(ctx, attempt-1); err != nil {
				return lastFMResponse{}, err
			}
		}
		response, err := client.fetchOnce(ctx, address)
		if !errors.Is(err, errLastFMRetryable) {
			return response, err
		}
		lastErr = err
	}
	return lastFMResponse{}, fmt.Errorf("last.fm retries exhausted: %w", lastErr)
}

func (client LastFMClient) fetchOnce(ctx context.Context, address string) (lastFMResponse, error) {
	if err := client.limiter.Wait(ctx); err != nil {
		return lastFMResponse{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return lastFMResponse{}, fmt.Errorf("building last.fm request: %w", err)
	}
	request.Header.Set("User-Agent", userAgentPrefix+client.settings.Version)
	response, err := client.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return lastFMResponse{}, ctx.Err()
		}
		return lastFMResponse{}, fmt.Errorf("%w: %w", errLastFMRetryable, err)
	}
	defer func() { _ = response.Body.Close() }()
	return decodeLastFMResponse(response)
}

type lastFMTag struct {
	Name string `json:"name"`
}

type lastFMResponse struct {
	TopTags struct {
		Tags []lastFMTag `json:"tag"`
	} `json:"toptags"`
	Error   int    `json:"error"`
	Message string `json:"message"`
}

func (response lastFMResponse) tagNames() []string {
	names := make([]string, 0, len(response.TopTags.Tags))
	for _, tag := range response.TopTags.Tags {
		names = append(names, tag.Name)
	}
	return names
}

func decodeLastFMResponse(response *http.Response) (lastFMResponse, error) {
	body, err := io.ReadAll(io.LimitReader(response.Body, lastFMResponseLimit))
	if err != nil {
		return lastFMResponse{}, fmt.Errorf("%w: reading body: %w", errLastFMRetryable, err)
	}
	if isRetryableStatus(response.StatusCode) {
		return lastFMResponse{}, fmt.Errorf("%w: status %d", errLastFMRetryable, response.StatusCode)
	}
	var decoded lastFMResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return lastFMResponse{}, fmt.Errorf("decoding last.fm response (status %d): %w", response.StatusCode, err)
	}
	return classifyLastFMError(decoded, response.StatusCode)
}

func isRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func classifyLastFMError(decoded lastFMResponse, status int) (lastFMResponse, error) {
	switch {
	case decoded.Error == lastFMNotFound:
		return lastFMResponse{}, nil
	case slices.Contains(lastFMKeyInvalidCodes, decoded.Error):
		return lastFMResponse{}, fmt.Errorf("%w: %s", services.ErrLastFMKeyInvalid, decoded.Message)
	case slices.Contains(lastFMRetryableCodes, decoded.Error):
		return lastFMResponse{}, fmt.Errorf("%w: error %d %s", errLastFMRetryable, decoded.Error, decoded.Message)
	case decoded.Error != 0 || status >= http.StatusBadRequest:
		return lastFMResponse{}, fmt.Errorf("%w: status %d error %d %s", errLastFMRejected, status, decoded.Error, decoded.Message)
	}
	return decoded, nil
}

func filterTags(names []string, artist string) []string {
	kept := []string{}
	artistName := strings.ToLower(strings.TrimSpace(artist))
	for _, name := range names {
		tag := strings.ToLower(strings.TrimSpace(name))
		if isNoiseTag(tag, artistName) || slices.Contains(kept, tag) {
			continue
		}
		kept = append(kept, tag)
		if len(kept) == lastFMKeptTags {
			break
		}
	}
	return kept
}

func isNoiseTag(tag, artist string) bool {
	return tag == "" || tag == artist || yearTagPattern.MatchString(tag) || slices.Contains(lastFMNoiseTags, tag)
}

type ExponentialBackoff struct {
	random func() float64
	clock  StabilityClock
}

func NewExponentialBackoff(random func() float64, clock StabilityClock) ExponentialBackoff {
	return ExponentialBackoff{random: random, clock: clock}
}

func (backoff ExponentialBackoff) Wait(ctx context.Context, attempt int) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-backoff.clock.After(backoff.Delay(attempt)):
		return nil
	}
}

func (backoff ExponentialBackoff) Delay(attempt int) time.Duration {
	jitter := 1 + lastFMJitter*(2*backoff.random()-1)
	return time.Duration(float64(lastFMBaseDelay) * math.Pow(2, float64(attempt)) * jitter)
}

func NewLastFMLimiter() *rate.Limiter {
	return rate.NewLimiter(lastFMRequestsPerSecond, 1)
}
