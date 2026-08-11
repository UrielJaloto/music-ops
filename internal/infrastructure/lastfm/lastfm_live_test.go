//go:build live

package lastfm

import (
	"context"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/internal/infrastructure/runtime"
	"golang.org/x/time/rate"
)

func TestLastFMClient_Live(t *testing.T) {
	key, isSet := os.LookupEnv("MUSIC_OPS_LASTFM_API_KEY")
	if !isSet || key == "" {
		t.Fatal("MUSIC_OPS_LASTFM_API_KEY is required for live tests")
	}
	client := NewLastFMClient(LastFMSettings{APIKey: key, BaseURL: "https://ws.audioscrobbler.com/2.0/", Version: "live-test"}, &http.Client{Timeout: 15 * time.Second}, rate.NewLimiter(4, 1), NewExponentialBackoff(func() float64 { return 0.5 }, runtime.NewSystemClock()))

	tags, err := client.FetchArtistTags(context.Background(), "Queen")

	if err != nil || !slices.Contains(tags, "rock") {
		t.Fatalf("tags = %v err = %v", tags, err)
	}
}
