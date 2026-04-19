package tests

import (
	"testing"

	"github.com/avelrl/openai-compatible-tester/internal/config"
)

func TestRunnerFilterTestsRespectsSuiteTarget(t *testing.T) {
	runner := NewRunner(config.Config{
		Suite: config.SuiteConfig{Target: "llama_shim"},
	}, nil, []TestCase{
		{ID: "responses.basic"},
		{ID: "responses.previous_response.chain", Target: "llama_shim"},
		{ID: "responses.other_target", Target: "another"},
	})

	filtered := runner.filterTests()
	if len(filtered) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(filtered))
	}
	if filtered[0].ID != "responses.basic" || filtered[1].ID != "responses.previous_response.chain" {
		t.Fatalf("unexpected filtered tests: %#v", filtered)
	}
}
