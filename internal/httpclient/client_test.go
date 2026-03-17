package httpclient

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestRetryDelayFromHeaderValueSeconds(t *testing.T) {
	now := time.Unix(100, 0)
	delay, ok := RetryDelayFromHeaderValue("17", now)
	if !ok {
		t.Fatal("expected delay to parse")
	}
	if delay != 17*time.Second {
		t.Fatalf("delay=%v", delay)
	}
}

func TestRetryDelayFromHeaderValueEpochMillis(t *testing.T) {
	now := time.UnixMilli(1741383600000)
	delay, ok := RetryDelayFromHeaderValue("1741383665000", now)
	if !ok {
		t.Fatal("expected epoch milliseconds to parse")
	}
	if delay != 65*time.Second {
		t.Fatalf("delay=%v", delay)
	}
}

func TestRetryDelayFromHeadersPrefersRetryAfter(t *testing.T) {
	now := time.UnixMilli(1741383600000)
	headers := http.Header{}
	headers.Set("Retry-After", "12")
	headers.Set("X-RateLimit-Reset", "1741383665000")
	delay, ok := RetryDelayFromHeaders(headers, now)
	if !ok {
		t.Fatal("expected delay from headers")
	}
	if delay != 12*time.Second {
		t.Fatalf("delay=%v", delay)
	}
}

func TestShouldHonorRetryDelayCap(t *testing.T) {
	now := time.UnixMilli(1741383600000)
	headers := http.Header{}
	headers.Set("X-RateLimit-Reset", "1741383725000") // 125s later
	if shouldHonorRetryDelay(headers, now) {
		t.Fatal("expected long retry window to be rejected")
	}
}

func TestRetryDelayFromHeadersSupportsStandardRateLimitReset(t *testing.T) {
	now := time.Unix(100, 0)
	headers := http.Header{}
	headers.Set("RateLimit-Reset", "17")
	delay, ok := RetryDelayFromHeaders(headers, now)
	if !ok {
		t.Fatal("expected delay from RateLimit-Reset")
	}
	if delay != 17*time.Second {
		t.Fatalf("delay=%v", delay)
	}
}

func TestSleepRetryDelayHonorsImmediateRetryHint(t *testing.T) {
	headers := http.Header{}
	headers.Set("Retry-After", "0")

	start := time.Now()
	sleepRetryDelay(250*time.Millisecond, 100*time.Millisecond, 1, headers)
	elapsed := time.Since(start)
	if elapsed >= 50*time.Millisecond {
		t.Fatalf("expected immediate retry, elapsed=%v", elapsed)
	}
}

func TestRequestRateLimiterWaitsBetweenRequests(t *testing.T) {
	limiter := &requestRateLimiter{next: map[string]time.Time{}}
	ctx := context.Background()

	start := time.Now()
	if err := limiter.Wait(ctx, "nvidia", 1200); err != nil {
		t.Fatalf("first wait: %v", err)
	}
	if err := limiter.Wait(ctx, "nvidia", 1200); err != nil {
		t.Fatalf("second wait: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 45*time.Millisecond {
		t.Fatalf("expected limiter to wait, elapsed=%v", elapsed)
	}
}

func TestClientReserveRateLimitSkipsFirstRequestWait(t *testing.T) {
	client := NewWithHTTPClient("https://example.test", "", nil, BuildRetryConfig(1, 0, nil), &http.Client{})
	ctx := WithRateLimit(context.Background(), "profile", 60)

	reservedCtx, err := client.ReserveRateLimit(ctx)
	if err != nil {
		t.Fatalf("ReserveRateLimit: %v", err)
	}

	start := time.Now()
	if err := client.waitRateLimit(reservedCtx); err != nil {
		t.Fatalf("waitRateLimit: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed >= 50*time.Millisecond {
		t.Fatalf("expected reserved slot to skip first wait, elapsed=%v", elapsed)
	}
}

func TestBuildRetryConfigWithRateLimitDefaultsToGenericRetry(t *testing.T) {
	cfg := BuildRetryConfigWithRateLimit(3, 250, []int{429, 500}, 0, 0)
	if cfg.MaxAttempts != 3 {
		t.Fatalf("MaxAttempts=%d", cfg.MaxAttempts)
	}
	if cfg.RateLimitMaxAttempts != 3 {
		t.Fatalf("RateLimitMaxAttempts=%d, want 3", cfg.RateLimitMaxAttempts)
	}
	if cfg.RateLimitFallback != 250*time.Millisecond {
		t.Fatalf("RateLimitFallback=%v, want 250ms", cfg.RateLimitFallback)
	}
	if _, ok := cfg.RetryOnStatus[429]; !ok {
		t.Fatal("expected 429 in retry set")
	}
}

func TestRetryConfigUsesDedicatedRateLimitAttemptBudget(t *testing.T) {
	cfg := BuildRetryConfigWithRateLimit(2, 250, []int{429}, 5, 1500)
	if got := cfg.maxAttemptsForStatus(http.StatusTooManyRequests); got != 5 {
		t.Fatalf("429 attempts=%d, want 5", got)
	}
	if got := cfg.maxAttemptsForStatus(http.StatusInternalServerError); got != 2 {
		t.Fatalf("500 attempts=%d, want 2", got)
	}
	if got := cfg.attemptBudget(); got != 5 {
		t.Fatalf("attemptBudget=%d, want 5", got)
	}
	if cfg.RateLimitFallback != 1500*time.Millisecond {
		t.Fatalf("RateLimitFallback=%v, want 1500ms", cfg.RateLimitFallback)
	}
}
