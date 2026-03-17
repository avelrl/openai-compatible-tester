package tests

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/avelrl/openai-compatible-tester/internal/config"
	"github.com/avelrl/openai-compatible-tester/internal/httpclient"
)

func TestRunnerRetriesFailedTest(t *testing.T) {
	attempts := 0
	runner := NewRunner(config.Config{
		Suite: config.SuiteConfig{
			Passes:         1,
			Parallelism:    1,
			TimeoutSeconds: 1,
			Retry: config.RetryConfig{
				TestRetries: 1,
			},
		},
	}, nil, []TestCase{
		{
			ID:   "chat.basic",
			Name: "Chat basic",
			Run: func(ctx context.Context, rc RunContext) Result {
				attempts++
				if attempts == 1 {
					return Result{Status: StatusFail, ErrorType: "assert", ErrorMessage: "boom"}
				}
				return Result{Status: StatusPass}
			},
		},
	})

	results, err := runner.Run(context.Background(), []config.ModelProfile{{Name: "p1", ChatModel: "m", ResponsesModel: "m"}}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != StatusPass {
		t.Fatalf("expected pass, got %s", results[0].Status)
	}
	if results[0].Attempts != 2 {
		t.Fatalf("expected Attempts=2, got %d", results[0].Attempts)
	}
}

func TestRunnerDoesNotRetryUnsupported(t *testing.T) {
	attempts := 0
	runner := NewRunner(config.Config{
		Suite: config.SuiteConfig{
			Passes:         1,
			Parallelism:    1,
			TimeoutSeconds: 1,
			Retry: config.RetryConfig{
				TestRetries: 3,
			},
		},
	}, nil, []TestCase{
		{
			ID:   "responses.conversations",
			Name: "Responses conversations",
			Run: func(ctx context.Context, rc RunContext) Result {
				attempts++
				return Result{Status: StatusUnsupported, ErrorType: "endpoint_missing"}
			},
		},
	})

	results, err := runner.Run(context.Background(), []config.ModelProfile{{Name: "p1", ChatModel: "m", ResponsesModel: "m"}}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Attempts != 1 {
		t.Fatalf("expected Attempts=1, got %d", results[0].Attempts)
	}
}

func TestRunnerRetriesTimeoutThenPass(t *testing.T) {
	attempts := 0
	runner := NewRunner(config.Config{
		Suite: config.SuiteConfig{
			Passes:         1,
			Parallelism:    1,
			TimeoutSeconds: 1,
			Retry: config.RetryConfig{
				TestRetries: 2,
			},
		},
	}, nil, []TestCase{
		{
			ID:   "responses.structured.json_schema",
			Name: "Responses structured json_schema",
			Run: func(ctx context.Context, rc RunContext) Result {
				attempts++
				if attempts == 1 {
					return Result{Status: StatusTimeout, ErrorType: "timeout", ErrorMessage: "deadline exceeded"}
				}
				return Result{Status: StatusPass}
			},
		},
	})

	results, err := runner.Run(context.Background(), []config.ModelProfile{{Name: "p1", ChatModel: "m", ResponsesModel: "m"}}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if results[0].Status != StatusPass {
		t.Fatalf("expected pass, got %s", results[0].Status)
	}
	if results[0].Attempts != 2 {
		t.Fatalf("expected Attempts=2, got %d", results[0].Attempts)
	}
}

func TestRunnerFilterTestsSkipsExplicitlyDisabledTests(t *testing.T) {
	disabled := false
	runner := NewRunner(config.Config{
		Suite: config.SuiteConfig{
			Stream: config.Toggle{Enabled: true},
			Tests: map[string]config.TestOverride{
				"responses.store_get": {Enabled: &disabled},
			},
		},
	}, nil, []TestCase{
		{ID: "responses.store_get", Name: "Responses store + GET"},
		{ID: "chat.basic", Name: "Chat basic"},
	})

	filtered := runner.filterTests()
	if len(filtered) != 1 {
		t.Fatalf("expected 1 test after filtering, got %d", len(filtered))
	}
	if filtered[0].ID != "chat.basic" {
		t.Fatalf("expected chat.basic to remain, got %s", filtered[0].ID)
	}
}

func TestRunnerRateLimitWaitDoesNotConsumeFirstRequestTimeout(t *testing.T) {
	client := httpclient.NewWithHTTPClient(
		"https://example.test",
		"",
		nil,
		httpclient.BuildRetryConfig(1, 0, nil),
		&http.Client{
			Timeout: 5 * time.Second,
			Transport: runTestRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				}, nil
			}),
		},
	)

	runner := NewRunner(config.Config{
		Suite: config.SuiteConfig{
			Passes:         1,
			Parallelism:    1,
			TimeoutSeconds: 1,
		},
	}, client, []TestCase{
		{
			ID:   "sanity.models",
			Name: "Models",
			Run: func(ctx context.Context, rc RunContext) Result {
				resp, err := rc.Client.Get(ctx, "/v1/models", nil)
				if err != nil {
					return Result{Status: StatusFail, ErrorType: "http_error", ErrorMessage: err.Error()}
				}
				if resp.StatusCode != http.StatusOK {
					return Result{Status: StatusFail, ErrorType: "http_status", ErrorMessage: "unexpected status"}
				}
				return Result{Status: StatusPass}
			},
		},
	})

	primeCtx := httpclient.WithRateLimit(context.Background(), "p1", 60)
	if _, err := client.ReserveRateLimit(primeCtx); err != nil {
		t.Fatalf("prime ReserveRateLimit: %v", err)
	}

	results, err := runner.Run(context.Background(), []config.ModelProfile{{
		Name:               "p1",
		ChatModel:          "m",
		ResponsesModel:     "m",
		RateLimitPerMinute: 60,
	}}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != StatusPass {
		t.Fatalf("expected pass, got %s (%s)", results[0].Status, results[0].ErrorMessage)
	}
}

type runTestRoundTripFunc func(*http.Request) (*http.Response, error)

func (f runTestRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
