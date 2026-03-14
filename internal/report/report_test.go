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
	if !strings.Contains(string(summaryMD), "Agent readiness") {
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
		{TestID: "chat.basic", Profile: "p1", Status: tests.StatusFail, HTTPStatus: 200, ErrorType: "assert", ErrorMessage: "expected OK"},
		{TestID: "chat.stream", Profile: "p1", Status: tests.StatusPass},
		{TestID: "chat.tool_call.required", Profile: "p1", Status: tests.StatusPass},
		{TestID: "responses.basic", Profile: "p1", Status: tests.StatusFail, HTTPStatus: 200, ErrorType: "assert", ErrorMessage: "expected OK"},
		{TestID: "responses.stream", Profile: "p1", Status: tests.StatusPass},
		{TestID: "responses.tool_call.required", Profile: "p1", Status: tests.StatusFail, HTTPStatus: 200, ErrorType: "tool_call", ErrorMessage: "missing function call"},
	}

	analysis := Analyze(results, cfg)

	if len(analysis.AgentReady) != 2 {
		t.Fatalf("expected 2 readiness entries, got %d", len(analysis.AgentReady))
	}

	var chatReady, responsesReady *AgentReadiness
	for i := range analysis.AgentReady {
		entry := &analysis.AgentReady[i]
		switch entry.Surface {
		case "chat":
			chatReady = entry
		case "responses":
			responsesReady = entry
		}
	}

	if chatReady == nil || responsesReady == nil {
		t.Fatalf("missing readiness entries: %+v", analysis.AgentReady)
	}
	if chatReady.Verdict != "READY WITH LIMITATIONS" {
		t.Fatalf("unexpected chat verdict: %+v", *chatReady)
	}
	if responsesReady.Verdict != "NOT READY" {
		t.Fatalf("unexpected responses verdict: %+v", *responsesReady)
	}

	if len(analysis.BasicExactness) != 2 {
		t.Fatalf("expected 2 basic exactness entries, got %d", len(analysis.BasicExactness))
	}
	for _, entry := range analysis.BasicExactness {
		if !entry.ProtocolCompatible {
			t.Fatalf("expected protocol-compatible basic path, got %+v", entry)
		}
		if entry.ExactMatch {
			t.Fatalf("expected exact-match failure, got %+v", entry)
		}
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
	if len(analysis.ClientCompat) != 1 {
		t.Fatalf("expected 1 client compatibility row, got %d", len(analysis.ClientCompat))
	}

	row := analysis.ClientCompat[0]
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

func TestAnalyzeFlakinessOnlyCountsMixedMultiRunResults(t *testing.T) {
	cfg := config.Config{}

	analysis := Analyze([]tests.Result{
		{TestID: "t1", TestName: "Test1", Profile: "p1", Status: tests.StatusFail},
	}, cfg)
	if len(analysis.Flaky) != 0 {
		t.Fatalf("single fail should not be considered flaky: %+v", analysis.Flaky)
	}

	analysis = Analyze([]tests.Result{
		{TestID: "t1", TestName: "Test1", Profile: "p1", Status: tests.StatusPass},
		{TestID: "t1", TestName: "Test1", Profile: "p1", Status: tests.StatusFail},
	}, cfg)
	if len(analysis.Flaky) != 1 {
		t.Fatalf("expected mixed multi-run result to be flaky: %+v", analysis.Flaky)
	}
}
