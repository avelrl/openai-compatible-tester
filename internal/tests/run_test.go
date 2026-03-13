package tests

import (
	"context"
	"testing"

	"github.com/avelrl/openai-compatible-tester/internal/config"
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
