package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avelrl/openai-compatible-tester/internal/config"
	"github.com/avelrl/openai-compatible-tester/internal/tests"
)

func TestWriteReports(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		BaseURL: "https://example.com",
		Suite:   config.SuiteConfig{Analysis: config.AnalysisConfig{ComputePercentiles: true, Percentiles: []int{50, 95, 99}}},
		Clients: config.ClientsConfig{Targets: []config.ClientTarget{
			{
				ID:       "codex-cli",
				Name:     "Codex CLI",
				Category: "coding_agent",
				Modes: []config.ClientMode{
					{
						Name:          "responses",
						API:           "responses",
						VerifiedOn:    "2026-03-13",
						Source:        "official_docs",
						Confidence:    "high",
						RequiredTests: []string{"t1"},
					},
				},
			},
		}},
	}
	results := []tests.Result{
		{TestID: "t1", TestName: "Test1", Profile: "p1", Pass: 1, Status: tests.StatusPass, LatencyMS: 10, TraceSteps: []tests.TraceStep{{Name: "main", Request: "{\"ok\":true}", Response: "{\"ok\":true}"}}},
		{TestID: "t1", TestName: "Test1", Profile: "p1", Pass: 2, Status: tests.StatusFail, LatencyMS: 20},
		{TestID: "t2", TestName: "Test2", Profile: "p1", Pass: 1, Status: tests.StatusUnsupported, ErrorType: "endpoint_missing", ErrorMessage: "missing endpoint"},
	}
	summary := Summary{Results: results, Config: cfg, Profiles: []config.ModelProfile{{Name: "p1"}}, StartedAt: time.Now(), EndedAt: time.Now()}
	analysis := Analyze(results, cfg)
	if err := WriteCSV(dir, results); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	if err := WriteSummaryMarkdown(dir, summary, analysis); err != nil {
		t.Fatalf("WriteSummaryMarkdown: %v", err)
	}
	if err := WriteFullLogJSONL(dir, results); err != nil {
		t.Fatalf("WriteFullLogJSONL: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "raw.csv")); err != nil {
		t.Fatalf("raw.csv missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "summary.md")); err != nil {
		t.Fatalf("summary.md missing: %v", err)
	}
	fullLogPath := filepath.Join(dir, "full_log.jsonl")
	if _, err := os.Stat(fullLogPath); err != nil {
		t.Fatalf("full_log.jsonl missing: %v", err)
	}
	b, err := os.ReadFile(fullLogPath)
	if err != nil {
		t.Fatalf("read full_log.jsonl: %v", err)
	}
	content := string(b)
	if !strings.Contains(content, "\"run_index\"") {
		t.Fatalf("full_log.jsonl missing run_index field: %s", content)
	}
	if !strings.Contains(content, "\"steps\"") {
		t.Fatalf("full_log.jsonl missing steps field: %s", content)
	}
	if !strings.Contains(content, "\"name\":\"main\"") {
		t.Fatalf("full_log.jsonl missing step name: %s", content)
	}

	summaryMD, err := os.ReadFile(filepath.Join(dir, "summary.md"))
	if err != nil {
		t.Fatalf("read summary.md: %v", err)
	}
	if !strings.Contains(string(summaryMD), "OpenAI spec conformance") {
		t.Fatalf("summary.md missing spec block: %s", string(summaryMD))
	}
	if !strings.Contains(string(summaryMD), "Compatibility") {
		t.Fatalf("summary.md missing compatibility block: %s", string(summaryMD))
	}
	if !strings.Contains(string(summaryMD), "Agent readiness") && !strings.Contains(string(summaryMD), "_Agent readiness_") {
		t.Fatalf("summary.md missing agent-readiness section: %s", string(summaryMD))
	}
	if !strings.Contains(string(summaryMD), "Known client compatibility") {
		t.Fatalf("summary.md missing known-client section: %s", string(summaryMD))
	}
	if !strings.Contains(string(summaryMD), "Basic text exactness") {
		t.Fatalf("summary.md missing basic-exactness section: %s", string(summaryMD))
	}
	if !strings.Contains(string(summaryMD), "Unsupported features") {
		t.Fatalf("summary.md missing unsupported-features section: %s", string(summaryMD))
	}
	if !strings.Contains(string(summaryMD), "endpoint_missing") {
		t.Fatalf("summary.md missing unsupported feature details: %s", string(summaryMD))
	}

	rawCSV, err := os.ReadFile(filepath.Join(dir, "raw.csv"))
	if err != nil {
		t.Fatalf("read raw.csv: %v", err)
	}
	if !strings.HasPrefix(string(rawCSV), "profile,run_index,attempts,") {
		t.Fatalf("raw.csv missing run_index header: %s", string(rawCSV))
	}
}

func TestAnalyzeSurfaceReadinessSeparatesProtocolFromExactness(t *testing.T) {
	cfg := config.Config{
		Suite: config.SuiteConfig{
			Stream:      config.Toggle{Enabled: true},
			ToolCalling: config.Toggle{Enabled: true},
		},
	}

	results := []tests.Result{
		{TestID: "sanity.models", Profile: "p1", Status: tests.StatusPass},
		{TestID: "chat.basic", Profile: "p1", Status: tests.StatusFail, HTTPStatus: 200, ErrorType: "assert", ErrorMessage: "expected OK", Evidence: &tests.Evidence{CanonicalTextSeen: true}},
		{TestID: "chat.stream", Profile: "p1", Status: tests.StatusPass, Evidence: &tests.Evidence{CanonicalStreamTextSeen: true}},
		{TestID: "chat.tool_call.required", Profile: "p1", Status: tests.StatusPass, Evidence: &tests.Evidence{CanonicalToolCallSeen: true}},
		{TestID: "responses.basic", Profile: "p1", Status: tests.StatusFail, HTTPStatus: 200, ErrorType: "assert", ErrorMessage: "expected OK", Evidence: &tests.Evidence{CanonicalTextSeen: true}},
		{TestID: "responses.stream", Profile: "p1", Status: tests.StatusPass, Evidence: &tests.Evidence{CanonicalStreamTextSeen: true}},
		{TestID: "responses.tool_call.required", Profile: "p1", Status: tests.StatusFail, HTTPStatus: 200, ErrorType: "tool_call", ErrorMessage: "missing function call"},
	}

	analysis := Analyze(results, cfg)

	if len(analysis.Compatibility.AgentReady) != 2 {
		t.Fatalf("expected 2 readiness entries, got %d", len(analysis.Compatibility.AgentReady))
	}

	var chatReady, responsesReady *AgentReadiness
	for i := range analysis.Compatibility.AgentReady {
		entry := &analysis.Compatibility.AgentReady[i]
		switch entry.Surface {
		case "chat":
			chatReady = entry
		case "responses":
			responsesReady = entry
		}
	}

	if chatReady == nil || responsesReady == nil {
		t.Fatalf("missing readiness entries: %+v", analysis.Compatibility.AgentReady)
	}
	if chatReady.Verdict != "READY WITH LIMITATIONS" {
		t.Fatalf("unexpected chat verdict: %+v", *chatReady)
	}
	if responsesReady.Verdict != "NOT READY" {
		t.Fatalf("unexpected responses verdict: %+v", *responsesReady)
	}

	if len(analysis.Compatibility.BasicExactness) != 2 {
		t.Fatalf("expected 2 basic exactness entries, got %d", len(analysis.Compatibility.BasicExactness))
	}
	for _, entry := range analysis.Compatibility.BasicExactness {
		if !entry.ProtocolCompatible {
			t.Fatalf("expected protocol-compatible basic path, got %+v", entry)
		}
		if entry.ExactMatch {
			t.Fatalf("expected exact-match failure, got %+v", entry)
		}
	}
	if len(analysis.Spec.SurfaceReadiness) != 2 {
		t.Fatalf("expected 2 strict readiness entries, got %d", len(analysis.Spec.SurfaceReadiness))
	}
	for _, entry := range analysis.Spec.SurfaceReadiness {
		if entry.Verdict != "READY" && entry.Surface == "chat" {
			t.Fatalf("unexpected strict chat verdict: %+v", entry)
		}
	}
}

func TestAnalyzeSurfaceReadinessNotesAutoOnlyResponsesToolCalling(t *testing.T) {
	cfg := config.Config{
		Suite: config.SuiteConfig{
			Stream:      config.Toggle{Enabled: true},
			ToolCalling: config.Toggle{Enabled: true},
		},
	}

	results := []tests.Result{
		{TestID: "responses.basic", Profile: "p1", Status: tests.StatusPass, Evidence: &tests.Evidence{CanonicalTextSeen: true}},
		{TestID: "responses.stream", Profile: "p1", Status: tests.StatusPass, Evidence: &tests.Evidence{CanonicalStreamTextSeen: true}},
		{TestID: "responses.tool_call", Profile: "p1", Status: tests.StatusPass, ToolChoiceMode: "auto", FunctionCallObserved: true, Evidence: &tests.Evidence{CanonicalToolCallSeen: true}},
		{TestID: "responses.tool_call.required", Profile: "p1", Status: tests.StatusFail, ErrorType: "http_status"},
	}

	analysis := Analyze(results, cfg)

	var responsesReady *AgentReadiness
	for i := range analysis.Compatibility.AgentReady {
		entry := &analysis.Compatibility.AgentReady[i]
		if entry.Surface == "responses" {
			responsesReady = entry
			break
		}
	}
	if responsesReady == nil {
		t.Fatalf("missing responses readiness entry: %+v", analysis.Compatibility.AgentReady)
	}

	found := false
	for _, note := range responsesReady.Notes {
		if note == "function tools work only with tool_choice=\"auto\"" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing auto-only note: %+v", responsesReady)
	}
}

func TestAnalyzeClientCompatibilityChoosesBestMode(t *testing.T) {
	cfg := config.Config{
		Clients: config.ClientsConfig{
			Targets: []config.ClientTarget{
				{
					ID:       "opencode",
					Name:     "OpenCode",
					Category: "coding_agent",
					Modes: []config.ClientMode{
						{
							Name:          "chat",
							API:           "chat",
							VerifiedOn:    "2026-03-13",
							Source:        "repo_docs",
							Confidence:    "medium",
							RequiredTests: []string{"chat.basic", "chat.stream", "chat.tool_call.required"},
						},
						{
							Name:          "responses",
							API:           "responses",
							VerifiedOn:    "2026-03-13",
							Source:        "repo_docs",
							Confidence:    "medium",
							RequiredTests: []string{"responses.basic", "responses.stream", "responses.tool_call.required"},
						},
					},
				},
			},
		},
	}

	results := []tests.Result{
		{TestID: "chat.basic", Profile: "p1", Status: tests.StatusPass},
		{TestID: "chat.stream", Profile: "p1", Status: tests.StatusPass},
		{TestID: "chat.tool_call.required", Profile: "p1", Status: tests.StatusPass},
		{TestID: "responses.basic", Profile: "p1", Status: tests.StatusPass},
		{TestID: "responses.stream", Profile: "p1", Status: tests.StatusPass},
		{TestID: "responses.tool_call.required", Profile: "p1", Status: tests.StatusFail, ErrorType: "tool_call"},
	}

	analysis := Analyze(results, cfg)
	if len(analysis.Compatibility.ClientCompat) != 1 {
		t.Fatalf("expected 1 client compatibility row, got %d", len(analysis.Compatibility.ClientCompat))
	}

	row := analysis.Compatibility.ClientCompat[0]
	if row.TargetID != "opencode" {
		t.Fatalf("unexpected target id: %+v", row)
	}
	if row.ModeName != "chat" || row.API != "chat" {
		t.Fatalf("expected chat mode to win, got %+v", row)
	}
	if row.Source != "repo_docs" || row.Confidence != "medium" || row.VerifiedOn != "2026-03-13" {
		t.Fatalf("unexpected verification metadata: %+v", row)
	}
	if row.Verdict != "COMPATIBLE" {
		t.Fatalf("unexpected verdict: %+v", row)
	}
	if row.RequiredPassed != 3 || row.RequiredTotal != 3 {
		t.Fatalf("unexpected required coverage: %+v", row)
	}
}

func TestAnalyzeClientCompatibilityTargetNotesDowngradeVerdictToLimitations(t *testing.T) {
	cfg := config.Config{
		Clients: config.ClientsConfig{
			Targets: []config.ClientTarget{
				{
					ID:       "codex-cli",
					Name:     "Codex CLI",
					Category: "coding_agent",
					Notes:    []string{"non-function tools may still fail in real use"},
					Modes: []config.ClientMode{
						{
							Name:          "responses",
							API:           "responses",
							VerifiedOn:    "2026-03-13",
							Source:        "official_docs",
							Confidence:    "high",
							RequiredTests: []string{"responses.basic", "responses.stream", "responses.tool_call.required", "responses.custom_tool"},
						},
					},
				},
			},
		},
	}

	results := []tests.Result{
		{TestID: "responses.basic", Profile: "p1", Status: tests.StatusPass},
		{TestID: "responses.stream", Profile: "p1", Status: tests.StatusPass},
		{TestID: "responses.tool_call.required", Profile: "p1", Status: tests.StatusPass},
		{TestID: "responses.custom_tool", Profile: "p1", Status: tests.StatusPass},
	}

	analysis := Analyze(results, cfg)
	if len(analysis.Compatibility.ClientCompat) != 1 {
		t.Fatalf("expected 1 client compatibility row, got %d", len(analysis.Compatibility.ClientCompat))
	}

	row := analysis.Compatibility.ClientCompat[0]
	if row.Verdict != "COMPATIBLE WITH LIMITATIONS" {
		t.Fatalf("expected target notes to downgrade verdict, got %+v", row)
	}
	if len(row.Notes) != 1 || row.Notes[0] != "non-function tools may still fail in real use" {
		t.Fatalf("unexpected notes: %+v", row)
	}
}

func TestAnalyzeFlakinessOnlyCountsMixedMultiRunResults(t *testing.T) {
	cfg := config.Config{}

	analysis := Analyze([]tests.Result{
		{TestID: "t1", TestName: "Test1", Profile: "p1", Status: tests.StatusFail},
	}, cfg)
	if len(analysis.Compatibility.Flaky) != 0 {
		t.Fatalf("single fail should not be considered flaky: %+v", analysis.Compatibility.Flaky)
	}
	if len(analysis.Spec.Flaky) != 0 {
		t.Fatalf("single fail should not be considered strictly flaky: %+v", analysis.Spec.Flaky)
	}

	analysis = Analyze([]tests.Result{
		{TestID: "t1", TestName: "Test1", Profile: "p1", Status: tests.StatusPass},
		{TestID: "t1", TestName: "Test1", Profile: "p1", Status: tests.StatusFail},
	}, cfg)
	if len(analysis.Compatibility.Flaky) != 1 {
		t.Fatalf("expected mixed multi-run result to be flaky: %+v", analysis.Compatibility.Flaky)
	}
	if len(analysis.Spec.Flaky) != 1 {
		t.Fatalf("expected mixed multi-run result to be strictly flaky: %+v", analysis.Spec.Flaky)
	}
}

func TestAnalyzeFlakinessSeparatesStrictFromCompat(t *testing.T) {
	cfg := config.Config{}

	analysis := Analyze([]tests.Result{
		{TestID: "responses.basic", TestName: "Responses basic", Profile: "p1", Status: tests.StatusPass, Evidence: &tests.Evidence{FallbackChatShapeOnResponses: true}},
		{TestID: "responses.basic", TestName: "Responses basic", Profile: "p1", Status: tests.StatusPass, Evidence: &tests.Evidence{CanonicalTextSeen: true}},
	}, cfg)

	if len(analysis.Compatibility.Flaky) != 0 {
		t.Fatalf("did not expect compat flakiness: %+v", analysis.Compatibility.Flaky)
	}
	if len(analysis.Spec.Flaky) != 1 {
		t.Fatalf("expected strict-only flakiness: %+v", analysis.Spec.Flaky)
	}
}

func TestWriteReportsIncludesRequiredToolCallingRows(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		Suite: config.SuiteConfig{
			Stream:      config.Toggle{Enabled: true},
			ToolCalling: config.Toggle{Enabled: true},
			Analysis:    config.AnalysisConfig{ComputePercentiles: true, Percentiles: []int{50, 95, 99}},
		},
	}
	results := []tests.Result{
		{TestID: "responses.basic", TestName: "Responses basic", Profile: "p1", Status: tests.StatusPass, Evidence: &tests.Evidence{CanonicalTextSeen: true}},
		{TestID: "responses.stream", TestName: "Responses stream", Profile: "p1", Status: tests.StatusPass, Evidence: &tests.Evidence{CanonicalStreamTextSeen: true}},
		{TestID: "responses.tool_call", TestName: "Responses tool calling", Profile: "p1", Status: tests.StatusPass, ToolChoiceMode: "auto", EffectiveToolChoice: "auto", ReasoningEffort: "low", LiteLLMTimeout: "60", FunctionCallObserved: true, Evidence: &tests.Evidence{CanonicalToolCallSeen: true}},
		{TestID: "responses.tool_call.required", TestName: "Responses tool calling (required)", Profile: "p1", Status: tests.StatusFail, ToolChoiceMode: "required", EffectiveToolChoice: "auto", ToolChoiceFallback: true, ReasoningEffort: "low", LiteLLMTimeout: "60"},
	}
	summary := Summary{Results: results, Config: cfg, Profiles: []config.ModelProfile{{Name: "p1"}}, StartedAt: time.Now(), EndedAt: time.Now()}
	analysis := Analyze(results, cfg)

	if err := WriteSummaryMarkdown(dir, summary, analysis); err != nil {
		t.Fatalf("WriteSummaryMarkdown: %v", err)
	}

	summaryMD, err := os.ReadFile(filepath.Join(dir, "summary.md"))
	if err != nil {
		t.Fatalf("read summary.md: %v", err)
	}
	content := string(summaryMD)
	if !strings.Contains(content, "| responses.tool_call.required | p1 |") {
		t.Fatalf("summary.md missing required tool row: %s", content)
	}
	if !strings.Contains(content, "| Test | Profile | P | F | U | T | function_call | requested | effective | fallback | reasoning | x-litellm-timeout |") {
		t.Fatalf("summary.md missing requested/effective/fallback header: %s", content)
	}
	if !strings.Contains(content, "| responses.tool_call.required | p1 | 0 | 1 | 0 | 0 | 0/1 | required | auto | yes | low | 60 |") {
		t.Fatalf("summary.md missing fallback detail row: %s", content)
	}
	if !strings.Contains(content, "function tools work only with tool_choice=\"auto\"") {
		t.Fatalf("summary.md missing auto-only detail: %s", content)
	}
}
