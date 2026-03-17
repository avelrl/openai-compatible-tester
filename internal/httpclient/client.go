package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/avelrl/openai-compatible-tester/internal/sse"
)

const maxSuggestedRetryDelay = 10 * time.Second

type rateLimitContextKey struct{}
type rateLimitReservationContextKey struct{}

type rateLimitSpec struct {
	Key               string
	RequestsPerMinute int
}

type rateLimitReservation struct {
	mu   sync.Mutex
	key  string
	used bool
}

type requestRateLimiter struct {
	mu   sync.Mutex
	next map[string]time.Time
}

var globalRateLimiter = &requestRateLimiter{next: map[string]time.Time{}}

type RetryConfig struct {
	MaxAttempts          int
	Backoff              time.Duration
	RetryOnStatus        map[int]struct{}
	RateLimitMaxAttempts int
	RateLimitFallback    time.Duration
}

type Client struct {
	baseURL        string
	apiKey         string
	defaultHeaders map[string]string
	retry          RetryConfig
	client         *http.Client
}

func WithRateLimit(ctx context.Context, key string, requestsPerMinute int) context.Context {
	if ctx == nil || strings.TrimSpace(key) == "" || requestsPerMinute <= 0 {
		return ctx
	}
	spec := rateLimitSpec{
		Key:               strings.TrimSpace(key),
		RequestsPerMinute: requestsPerMinute,
	}
	return context.WithValue(ctx, rateLimitContextKey{}, spec)
}

func (c *Client) ReserveRateLimit(ctx context.Context) (context.Context, error) {
	spec, ok := rateLimitFromContext(ctx)
	if !ok || spec.RequestsPerMinute <= 0 {
		return ctx, nil
	}
	key := c.baseURL + "|" + spec.Key
	if err := globalRateLimiter.Wait(ctx, key, spec.RequestsPerMinute); err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, rateLimitReservationContextKey{}, &rateLimitReservation{key: key}), nil
}

type Response struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
	Latency    time.Duration
	BytesOut   int64
	BytesIn    int64
}

type StreamResult struct {
	Response
	Done bool
}

func New(baseURL, apiKey string, defaultHeaders map[string]string, timeout time.Duration, retry RetryConfig) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = 10
	return NewWithHTTPClient(
		baseURL,
		apiKey,
		defaultHeaders,
		retry,
		&http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	)
}

func NewWithHTTPClient(baseURL, apiKey string, defaultHeaders map[string]string, retry RetryConfig, client *http.Client) *Client {
	if client == nil {
		client = &http.Client{}
	}
	return &Client{
		baseURL:        strings.TrimRight(baseURL, "/"),
		apiKey:         apiKey,
		defaultHeaders: defaultHeaders,
		retry:          retry,
		client:         client,
	}
}

func (c *Client) Get(ctx context.Context, path string, headers map[string]string) (*Response, error) {
	url := c.baseURL + path
	return c.do(ctx, http.MethodGet, url, nil, headers)
}

func (c *Client) PostJSON(ctx context.Context, path string, payload interface{}, headers map[string]string) (*Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	url := c.baseURL + path
	return c.do(ctx, http.MethodPost, url, body, headers)
}

func (c *Client) PostJSONStream(ctx context.Context, path string, payload interface{}, headers map[string]string, onEvent func(sse.Event) error) (*StreamResult, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	url := c.baseURL + path
	var lastErr error
	for attempt := 1; attempt <= c.retry.attemptBudget(); attempt++ {
		if err := c.waitRateLimit(ctx); err != nil {
			return nil, err
		}
		start := time.Now()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		c.applyHeaders(req, headers)
		resp, err := c.client.Do(req)
		if err != nil {
			if shouldRetryErr(err) && attempt < c.retry.MaxAttempts {
				lastErr = err
				sleepBackoff(c.retry.Backoff, attempt)
				continue
			}
			return nil, err
		}
		// ensure body closed on all paths
		result := &StreamResult{Response: Response{StatusCode: resp.StatusCode, Headers: resp.Header, Latency: time.Since(start), BytesOut: int64(len(body))}}
		if resp.StatusCode >= 400 {
			data, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			result.Body = data
			result.BytesIn = int64(len(data))
			if resp.StatusCode == http.StatusTooManyRequests && attempt < c.retry.maxAttemptsForStatus(resp.StatusCode) {
				if !shouldHonorRetryDelay(resp.Header, time.Now()) {
					return result, nil
				}
				lastErr = fmt.Errorf("http %d", resp.StatusCode)
				sleepRetryDelay(c.retry.Backoff, c.retry.RateLimitFallback, attempt, resp.Header)
				continue
			}
			if shouldRetryStatus(resp.StatusCode, c.retry) && attempt < c.retry.maxAttemptsForStatus(resp.StatusCode) {
				if !shouldHonorRetryDelay(resp.Header, time.Now()) {
					return result, nil
				}
				lastErr = fmt.Errorf("http %d", resp.StatusCode)
				sleepRetryDelay(c.retry.Backoff, 0, attempt, resp.Header)
				continue
			}
			return result, nil
		}
		counting := &countingReader{r: resp.Body}
		err = sse.Parse(counting, func(ev sse.Event) error {
			return onEvent(ev)
		})
		_ = resp.Body.Close()
		result.BytesIn = counting.n
		if err != nil {
			if shouldRetryErr(err) && attempt < c.retry.MaxAttempts {
				lastErr = err
				sleepBackoff(c.retry.Backoff, attempt)
				continue
			}
			return result, err
		}
		result.Done = true
		return result, nil
	}
	return nil, lastErr
}

func (c *Client) do(ctx context.Context, method, url string, body []byte, headers map[string]string) (*Response, error) {
	var lastErr error
	for attempt := 1; attempt <= c.retry.attemptBudget(); attempt++ {
		if err := c.waitRateLimit(ctx); err != nil {
			return nil, err
		}
		start := time.Now()
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, reader)
		if err != nil {
			return nil, err
		}
		c.applyHeaders(req, headers)
		resp, err := c.client.Do(req)
		if err != nil {
			if shouldRetryErr(err) && attempt < c.retry.MaxAttempts {
				lastErr = err
				sleepBackoff(c.retry.Backoff, attempt)
				continue
			}
			return nil, err
		}
		data, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		result := &Response{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       data,
			Latency:    time.Since(start),
			BytesOut:   int64(len(body)),
			BytesIn:    int64(len(data)),
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt < c.retry.maxAttemptsForStatus(resp.StatusCode) {
			if !shouldHonorRetryDelay(resp.Header, time.Now()) {
				return result, nil
			}
			lastErr = fmt.Errorf("http %d", resp.StatusCode)
			sleepRetryDelay(c.retry.Backoff, c.retry.RateLimitFallback, attempt, resp.Header)
			continue
		}
		if shouldRetryStatus(resp.StatusCode, c.retry) && attempt < c.retry.maxAttemptsForStatus(resp.StatusCode) {
			if !shouldHonorRetryDelay(resp.Header, time.Now()) {
				return result, nil
			}
			lastErr = fmt.Errorf("http %d", resp.StatusCode)
			sleepRetryDelay(c.retry.Backoff, 0, attempt, resp.Header)
			continue
		}
		return result, nil
	}
	return nil, lastErr
}

func (c *Client) applyHeaders(req *http.Request, headers map[string]string) {
	for k, v := range c.defaultHeaders {
		if req.Header.Get(k) == "" {
			req.Header.Set(k, v)
		}
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" && req.Header.Get("Authorization") == "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

func shouldRetryErr(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return false
}

func shouldRetryStatus(code int, cfg RetryConfig) bool {
	if len(cfg.RetryOnStatus) == 0 {
		return false
	}
	_, ok := cfg.RetryOnStatus[code]
	return ok
}

func sleepBackoff(base time.Duration, attempt int) {
	if base <= 0 {
		return
	}
	jitter := time.Duration(attempt-1) * base
	time.Sleep(base + jitter)
}

func sleepRetryDelay(base, fallback time.Duration, attempt int, headers http.Header) {
	if delay, ok := RetryDelayFromHeaders(headers, time.Now()); ok {
		if delay <= 0 {
			return
		}
		if delay > maxSuggestedRetryDelay {
			sleepBackoff(base, attempt)
			return
		}
		time.Sleep(delay)
		return
	}
	if fallback > 0 {
		time.Sleep(fallback)
		return
	}
	sleepBackoff(base, attempt)
}

func shouldHonorRetryDelay(headers http.Header, now time.Time) bool {
	delay, ok := RetryDelayFromHeaders(headers, now)
	if !ok {
		return true
	}
	return delay <= maxSuggestedRetryDelay
}

func RetryDelayFromHeaders(headers http.Header, now time.Time) (time.Duration, bool) {
	for _, key := range []string{"Retry-After", "retry-after"} {
		if raw := strings.TrimSpace(headers.Get(key)); raw != "" {
			if delay, ok := RetryDelayFromHeaderValue(raw, now); ok {
				return delay, true
			}
		}
	}
	for _, key := range []string{
		"RateLimit-Reset",
		"ratelimit-reset",
		"X-RateLimit-Reset",
		"x-ratelimit-reset",
	} {
		if raw := strings.TrimSpace(headers.Get(key)); raw != "" {
			if delay, ok := RetryDelayFromHeaderValue(raw, now); ok {
				return delay, true
			}
		}
	}
	return 0, false
}

func (c *Client) waitRateLimit(ctx context.Context) error {
	spec, ok := rateLimitFromContext(ctx)
	if !ok || spec.RequestsPerMinute <= 0 {
		return nil
	}
	key := c.baseURL + "|" + spec.Key
	if consumeRateLimitReservation(ctx, key) {
		return nil
	}
	return globalRateLimiter.Wait(ctx, key, spec.RequestsPerMinute)
}

func rateLimitFromContext(ctx context.Context) (rateLimitSpec, bool) {
	if ctx == nil {
		return rateLimitSpec{}, false
	}
	spec, ok := ctx.Value(rateLimitContextKey{}).(rateLimitSpec)
	if !ok || strings.TrimSpace(spec.Key) == "" || spec.RequestsPerMinute <= 0 {
		return rateLimitSpec{}, false
	}
	return spec, true
}

func consumeRateLimitReservation(ctx context.Context, key string) bool {
	if ctx == nil || strings.TrimSpace(key) == "" {
		return false
	}
	reservation, ok := ctx.Value(rateLimitReservationContextKey{}).(*rateLimitReservation)
	if !ok || reservation == nil || reservation.key != key {
		return false
	}
	reservation.mu.Lock()
	defer reservation.mu.Unlock()
	if reservation.used {
		return false
	}
	reservation.used = true
	return true
}

func (c RetryConfig) attemptBudget() int {
	budget := c.MaxAttempts
	if budget < 1 {
		budget = 1
	}
	if c.RateLimitMaxAttempts > budget {
		budget = c.RateLimitMaxAttempts
	}
	return budget
}

func (c RetryConfig) maxAttemptsForStatus(code int) int {
	if code == http.StatusTooManyRequests && c.RateLimitMaxAttempts > 0 {
		return c.RateLimitMaxAttempts
	}
	if c.MaxAttempts > 0 {
		return c.MaxAttempts
	}
	return 1
}

func (l *requestRateLimiter) Wait(ctx context.Context, key string, requestsPerMinute int) error {
	if ctx == nil || strings.TrimSpace(key) == "" || requestsPerMinute <= 0 {
		return nil
	}
	interval := time.Minute / time.Duration(requestsPerMinute)
	if interval <= 0 {
		return nil
	}
	for {
		l.mu.Lock()
		now := time.Now()
		readyAt := l.next[key]
		if !now.Before(readyAt) {
			l.next[key] = now.Add(interval)
			l.mu.Unlock()
			return nil
		}
		wait := readyAt.Sub(now)
		l.mu.Unlock()

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func RetryDelayFromHeaderValue(raw string, now time.Time) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if when, err := http.ParseTime(raw); err == nil {
		if !when.After(now) {
			return 0, true
		}
		return when.Sub(now), true
	}

	digits := onlyDigits(raw)
	if digits == "" {
		return 0, false
	}
	value, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, false
	}
	switch {
	case len(digits) >= 13:
		reset := time.UnixMilli(value)
		if !reset.After(now) {
			return 0, true
		}
		return reset.Sub(now), true
	case len(digits) >= 10:
		reset := time.Unix(value, 0)
		if !reset.After(now) {
			return 0, true
		}
		return reset.Sub(now), true
	default:
		seconds := int(value)
		if seconds <= 0 {
			return 0, true
		}
		return time.Duration(seconds) * time.Second, true
	}
}

func onlyDigits(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func BuildRetryConfig(maxAttempts int, backoffMS int, retryOn []int) RetryConfig {
	return BuildRetryConfigWithRateLimit(maxAttempts, backoffMS, retryOn, 0, 0)
}

func BuildRetryConfigWithRateLimit(maxAttempts int, backoffMS int, retryOn []int, rateLimitMaxAttempts int, rateLimitFallbackMS int) RetryConfig {
	set := map[int]struct{}{}
	for _, code := range retryOn {
		set[code] = struct{}{}
	}
	if rateLimitMaxAttempts <= 0 {
		rateLimitMaxAttempts = maxAttempts
	}
	if rateLimitFallbackMS < 0 {
		rateLimitFallbackMS = 0
	}
	if rateLimitFallbackMS == 0 {
		rateLimitFallbackMS = backoffMS
	}
	return RetryConfig{
		MaxAttempts:          maxAttempts,
		Backoff:              time.Duration(backoffMS) * time.Millisecond,
		RetryOnStatus:        set,
		RateLimitMaxAttempts: rateLimitMaxAttempts,
		RateLimitFallback:    time.Duration(rateLimitFallbackMS) * time.Millisecond,
	}
}
