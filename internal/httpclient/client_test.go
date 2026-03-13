package httpclient

import (
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
