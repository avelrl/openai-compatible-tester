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
	"time"

	"github.com/avelrl/openai-compatible-tester/internal/sse"
)

const maxSuggestedRetryDelay = 10 * time.Second

type RetryConfig struct {
	MaxAttempts   int
	Backoff       time.Duration
	RetryOnStatus map[int]struct{}
}

type Client struct {
	baseURL        string
	apiKey         string
	defaultHeaders map[string]string
	retry          RetryConfig
	client         *http.Client
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
	return &Client{
		baseURL:        strings.TrimRight(baseURL, "/"),
		apiKey:         apiKey,
		defaultHeaders: defaultHeaders,
		retry:          retry,
		client: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
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
	for attempt := 1; attempt <= c.retry.MaxAttempts; attempt++ {
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
			if shouldRetryStatus(resp.StatusCode, c.retry) && attempt < c.retry.MaxAttempts {
				if !shouldHonorRetryDelay(resp.Header, time.Now()) {
					return result, nil
				}
				lastErr = fmt.Errorf("http %d", resp.StatusCode)
				sleepRetryDelay(c.retry.Backoff, attempt, resp.Header)
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
	for attempt := 1; attempt <= c.retry.MaxAttempts; attempt++ {
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
		if resp.StatusCode == http.StatusTooManyRequests && attempt < c.retry.MaxAttempts {
			if !shouldHonorRetryDelay(resp.Header, time.Now()) {
				return result, nil
			}
			lastErr = fmt.Errorf("http %d", resp.StatusCode)
			sleepRetryDelay(c.retry.Backoff, attempt, resp.Header)
			continue
		}
		if shouldRetryStatus(resp.StatusCode, c.retry) && attempt < c.retry.MaxAttempts {
			if !shouldHonorRetryDelay(resp.Header, time.Now()) {
				return result, nil
			}
			lastErr = fmt.Errorf("http %d", resp.StatusCode)
			sleepRetryDelay(c.retry.Backoff, attempt, resp.Header)
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

func sleepRetryDelay(base time.Duration, attempt int, headers http.Header) {
	if delay, ok := RetryDelayFromHeaders(headers, time.Now()); ok && delay > 0 {
		if delay > maxSuggestedRetryDelay {
			sleepBackoff(base, attempt)
			return
		}
		time.Sleep(delay)
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
	for _, key := range []string{"X-RateLimit-Reset", "x-ratelimit-reset"} {
		if raw := strings.TrimSpace(headers.Get(key)); raw != "" {
			if delay, ok := RetryDelayFromHeaderValue(raw, now); ok {
				return delay, true
			}
		}
	}
	return 0, false
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
	set := map[int]struct{}{}
	for _, code := range retryOn {
		set[code] = struct{}{}
	}
	return RetryConfig{
		MaxAttempts:   maxAttempts,
		Backoff:       time.Duration(backoffMS) * time.Millisecond,
		RetryOnStatus: set,
	}
}
