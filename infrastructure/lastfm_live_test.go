//go:build live

package infrastructure

import (
	"context"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestLastFMClient_Live(t *testing.T) {
	key, isSet := os.LookupEnv("MUSIC_OPS_LASTFM_API_KEY")
	if !isSet || key == "" {
		t.Fatal("MUSIC_OPS_LASTFM_API_KEY is required for live tests")
	}
	client := NewLastFMClient(LastFMSettings{APIKey: key, BaseURL: "https://ws.audioscrobbler.com/2.0/", Version: "live-test"}, &http.Client{Timeout: 15 * time.Second}, rate.NewLimiter(4, 1), NewExponentialBackoff(func() float64 { return 0.5 }, NewSystemClock()))

	tags, err := client.ArtistTags(context.Background(), "Queen")

	if err != nil || !slices.Contains(tags, "rock") {
		t.Fatalf("tags = %v err = %v", tags, err)
	}
}
