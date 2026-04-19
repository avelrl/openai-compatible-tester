package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/avelrl/openai-compatible-tester/internal/config"
)

func TestRunnerBlocksDisabledCapabilityBeforeExecution(t *testing.T) {
	cfg := config.Config{
		Suite: config.SuiteConfig{
			Passes:      1,
			Parallelism: 1,
		},
		Capabilities: config.CapabilitiesConfig{
			Capabilities: map[string]config.CapabilitySpec{
				"tool.web_search.local": {
					Status: config.CapabilityStatusDisabled,
					Reason: "local backend is not configured",
				},
			},
		},
	}
	profiles := []config.ModelProfile{{
		Name:           "llama-shim",
		ChatModel:      "shim-chat",
		ResponsesModel: "shim-responses",
	}}
	test := TestCase{
		ID:                   "responses.tool.web_search.local",
		Name:                 "Local web search",
		Kind:                 KindResponses,
		RequiredCapabilities: []string{"tool.web_search.local"},
		Run: func(ctx context.Context, rc RunContext) Result {
			t.Fatal("test body should not execute when capability is disabled")
			return Result{}
		},
	}

	results, err := NewRunner(cfg, nil, []TestCase{test}).Run(context.Background(), profiles, nil)
	if err != nil {
		t.Fatalf("runner returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected exactly one result, got %d", len(results))
	}
	if results[0].Status != StatusUnsupported {
		t.Fatalf("unexpected status: %s", results[0].Status)
	}
	if results[0].ErrorType != ErrorTypeCapabilityDisabled {
		t.Fatalf("unexpected error type: %s", results[0].ErrorType)
	}
	if !strings.Contains(results[0].ErrorMessage, "tool.web_search.local") {
		t.Fatalf("expected capability name in error message, got %q", results[0].ErrorMessage)
	}
}

func TestProjectForModePreservesCapabilityGateAsUnsupportedInStrict(t *testing.T) {
	res := Result{
		TestID:       "responses.tool.web_search.local",
		Status:       StatusUnsupported,
		ErrorType:    ErrorTypeCapabilityDisabled,
		ErrorMessage: "required capability tool.web_search.local is disabled in this environment",
	}

	projected := ProjectForMode(res, config.ModeStrict)
	if projected.Status != StatusUnsupported {
		t.Fatalf("unexpected strict status: %s", projected.Status)
	}
	if projected.ErrorType != ErrorTypeCapabilityDisabled {
		t.Fatalf("unexpected strict error type: %s", projected.ErrorType)
	}
}
