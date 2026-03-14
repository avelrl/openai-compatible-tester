package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/avelrl/openai-compatible-tester/internal/config"
	"github.com/avelrl/openai-compatible-tester/internal/tests"
)

func TestExecuteReturnsZeroForHelp(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() {
		os.Args = oldArgs
	})

	os.Args = []string{"openai-compatible-tester", "-h"}
	if code := Execute(); code != 0 {
		t.Fatalf("Execute()=%d, want 0", code)
	}
}

func TestResolveRunOutDir(t *testing.T) {
	ts := time.Date(2026, 3, 13, 17, 4, 5, 0, time.UTC)
	profiles := []config.ModelProfile{{
		Name:           "openrouter-qwen35-9b",
		ChatModel:      "qwen/qwen3.5-9b",
		ResponsesModel: "qwen/qwen3.5-9b",
	}}

	got := resolveRunOutDir("reports", "https://openrouter.ai/api/v1", profiles, ts)
	want := "reports/openrouter_qwen35_9b_20260313_170405"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveRunOutDirUsesCustomOutDirAsPrefix(t *testing.T) {
	ts := time.Date(2026, 3, 13, 17, 4, 5, 0, time.UTC)
	profiles := []config.ModelProfile{{
		Name:           "openrouter-qwen35-9b",
		ChatModel:      "qwen/qwen3.5-9b",
		ResponsesModel: "qwen/qwen3.5-9b",
	}}

	got := resolveRunOutDir("reports/openrouter-qwen35-9b", "https://openrouter.ai/api/v1", profiles, ts)
	want := "reports/openrouter-qwen35-9b_20260313_170405"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRunProfileSegmentMulti(t *testing.T) {
	got := runProfileSegment("https://openrouter.ai/api/v1", []config.ModelProfile{
		{Name: "a", ResponsesModel: "one"},
		{Name: "b", ResponsesModel: "two"},
	})
	if got != "multi" {
		t.Fatalf("got %q", got)
	}
}

func TestRunProfileSegmentFallsBackToModel(t *testing.T) {
	got := runProfileSegment("", []config.ModelProfile{{
		ResponsesModel: "qwen/qwen3.5-9b",
		ChatModel:      "qwen/qwen3.5-9b",
	}})
	if got != "qwen_qwen3_5_9b" {
		t.Fatalf("got %q", got)
	}
}

func TestCollectSelectedTestIDsFromCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "raw.csv")
	content := "" +
		"profile,run_index,test_id,test_name,status,http_status,latency_ms,bytes_in,bytes_out,tokens,error_type,error_message,model,warmup,tool_choice_mode,reasoning_effort,litellm_timeout,function_call_observed\n" +
		"p1,1,chat.basic,Chat basic,fail,200,10,0,0,0,assert,expected OK,m,false,,,,false\n" +
		"p1,1,chat.stream,Chat stream,pass,200,10,0,0,0,,,,false,,,,false\n" +
		"p1,1,responses.tool_call.required,Responses tool,timeout,0,0,0,0,0,timeout,deadline,m,false,,,,false\n" +
		"p1,1,chat.basic,Chat basic,fail,200,10,0,0,0,assert,expected OK,m,false,,,,false\n" +
		"p1,1,responses.memory.prev_id,Responses memory,fail,200,10,0,0,0,assert,bad,m,true,,,,false\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := collectSelectedTestIDs("", path)
	if err != nil {
		t.Fatalf("collectSelectedTestIDs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 selected tests, got %d", len(got))
	}
	if _, ok := got["chat.basic"]; !ok {
		t.Fatalf("missing chat.basic")
	}
	if _, ok := got["responses.tool_call.required"]; !ok {
		t.Fatalf("missing responses.tool_call.required")
	}
	if _, ok := got["responses.memory.prev_id"]; ok {
		t.Fatalf("did not expect warmup failure to be selected")
	}
}

func TestCollectSelectedTestIDsMergesManualAndCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "raw.csv")
	content := "" +
		"profile,run_index,test_id,test_name,status,http_status,latency_ms,bytes_in,bytes_out,tokens,error_type,error_message,model,warmup,tool_choice_mode,reasoning_effort,litellm_timeout,function_call_observed\n" +
		"p1,1,chat.basic,Chat basic,fail,200,10,0,0,0,assert,expected OK,m,false,,,,false\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := collectSelectedTestIDs("responses.basic, chat.stream", path)
	if err != nil {
		t.Fatalf("collectSelectedTestIDs: %v", err)
	}
	for _, testID := range []string{"chat.basic", "responses.basic", "chat.stream"} {
		if _, ok := got[testID]; !ok {
			t.Fatalf("missing %s", testID)
		}
	}
}

func TestFilterTestsSkipsExplicitlyDisabledOverride(t *testing.T) {
	disabled := false
	cfg := config.Config{
		Suite: config.SuiteConfig{
			Stream: config.Toggle{Enabled: true},
			Tests: map[string]config.TestOverride{
				"responses.store_get": {Enabled: &disabled},
			},
		},
	}
	all := []tests.TestCase{
		{ID: "responses.store_get", Name: "Responses store + GET"},
		{ID: "chat.basic", Name: "Chat basic"},
	}

	filtered := filterTests(cfg, all, nil)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 test after filtering, got %d", len(filtered))
	}
	if filtered[0].ID != "chat.basic" {
		t.Fatalf("expected chat.basic to remain, got %s", filtered[0].ID)
	}
}
