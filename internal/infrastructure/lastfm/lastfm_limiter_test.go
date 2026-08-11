package lastfm

import "testing"

func TestNewLastFMLimiter_AllowsOneBurst(t *testing.T) {
	limiter := NewLastFMLimiter()

	if limiter.Burst() != 1 || float64(limiter.Limit()) != lastFMRequestsPerSecond {
		t.Fatalf("burst = %d limit = %v", limiter.Burst(), limiter.Limit())
	}
}
