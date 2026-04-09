package cmd

import (
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/avelrl/openai-compatible-tester/internal/config"
	"github.com/avelrl/openai-compatible-tester/internal/httpclient"
	"github.com/avelrl/openai-compatible-tester/internal/report"
	"github.com/avelrl/openai-compatible-tester/internal/tests"
	"github.com/avelrl/openai-compatible-tester/internal/tui"
)

func Execute() int {
	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	baseURL := fs.String("base-url", "", "Base URL for API server")
	apiKey := fs.String("api-key", "", "API key (overrides env)")
	suitePath := fs.String("suite", config.DefaultSuitePath, "Suite config YAML")
	modelsPath := fs.String("models", config.DefaultModelsPath, "Models config YAML")
	endpointsPath := fs.String("endpoints", config.DefaultEndpointsPath, "Endpoints config YAML")
	clientsPath := fs.String("clients", config.DefaultClientsPath, "Known clients compatibility YAML")
	mode := fs.String("mode", config.ModeCompat, "Primary verdict mode: compat or strict")
	profile := fs.String("profile", "", "Run only one model profile by name")
	testIDs := fs.String("tests", "", "Comma-separated test IDs to run")
	rerunFailuresFrom := fs.String("rerun-failures-from", "", "Path to raw.csv; rerun only FAIL/TIMEOUT tests from that report")
	passes := fs.Int("passes", 0, "Override passes")
	retryFails := fs.Int("retry-fails", -1, "Retry each FAIL/TIMEOUT test this many extra times; unsupported is not retried")
	noTUI := fs.Bool("no-tui", false, "Disable interactive TUI")
	jsonOut := fs.Bool("json", false, "Also emit reports/summary.json")
	noStream := fs.Bool("no-stream", false, "Disable streaming tests")
	analyze := fs.Bool("analyze", false, "Enable analysis")
	noAnalyze := fs.Bool("no-analyze", false, "Disable analysis")
	outDir := fs.String("out-dir", "reports", "Output directory")
	verbose := fs.Bool("verbose", false, "Verbose logging")
	envFile := fs.String("env-file", "", "Path to .env file")

	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	var analyzeOverride *bool
	if *analyze && *noAnalyze {
		fmt.Fprintln(os.Stderr, "--analyze and --no-analyze are mutually exclusive")
		return 2
	}
	if *analyze {
		val := true
		analyzeOverride = &val
	}
	if *noAnalyze {
		val := false
		analyzeOverride = &val
	}

	cfg, err := config.Load(config.LoadOptions{
		SuitePath:     *suitePath,
		ModelsPath:    *modelsPath,
		EndpointsPath: *endpointsPath,
		ClientsPath:   *clientsPath,
		EnvFile:       *envFile,
		Mode:          *mode,
		BaseURL:       *baseURL,
		APIKey:        *apiKey,
		Profile:       *profile,
		Passes:        *passes,
		RetryFails:    *retryFails,
		NoTUI:         *noTUI,
		JSON:          *jsonOut,
		NoStream:      *noStream,
		Analyze:       analyzeOverride,
		OutDir:        *outDir,
		Verbose:       *verbose,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		return 2
	}

	profiles, err := selectProfiles(cfg.Models.Profiles, cfg.Profile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	retryCfg := httpclient.BuildRetryConfigWithRateLimit(
		cfg.Suite.Retry.MaxAttempts,
		cfg.Suite.Retry.BackoffMS,
		cfg.Suite.Retry.RetryOnStatus,
		cfg.Suite.Retry.RateLimitMaxAttempts,
		cfg.Suite.Retry.RateLimitFallbackMS,
	)
	clientTimeoutSeconds := cfg.Suite.TimeoutSeconds
	for _, ov := range cfg.Suite.Tests {
		if ov.TimeoutSeconds > clientTimeoutSeconds {
			clientTimeoutSeconds = ov.TimeoutSeconds
		}
	}
	client := httpclient.New(cfg.BaseURL, cfg.APIKey, cfg.Endpoints.DefaultHeaders, time.Duration(clientTimeoutSeconds)*time.Second, retryCfg)

	allTests := tests.Registry()
	selectedTestIDs, err := collectSelectedTestIDs(*testIDs, *rerunFailuresFrom)
	if err != nil {
		fmt.Fprintln(os.Stderr, "test selection error:", err)
		return 2
	}
	filteredTests := filterTests(cfg, allTests, selectedTestIDs)
	if len(filteredTests) == 0 {
		fmt.Fprintln(os.Stderr, "no tests selected after filtering")
		return 2
	}
	runner := tests.NewRunner(cfg, client, filteredTests)

	var results []tests.Result
	start := time.Now()
	cfg.OutDir = resolveRunOutDir(cfg.OutDir, cfg.BaseURL, profiles, start)
	fmt.Fprintf(os.Stderr, "Writing reports to %s\n", cfg.OutDir)
	if len(selectedTestIDs) > 0 {
		fmt.Fprintf(os.Stderr, "Selected tests: %s\n", strings.Join(testIDsForDisplay(filteredTests), ", "))
	}
	if !cfg.NoTUI {
		res, err := tui.Run(context.Background(), runner, cfg, profiles, filteredTests, cfg.OutDir)
		if err == nil {
			results = res
		} else {
			fmt.Fprintln(os.Stderr, "TUI disabled:", err)
			cfg.NoTUI = true
		}
	}
	if cfg.NoTUI {
		res, err := runner.Run(context.Background(), profiles, nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, "run error:", err)
		}
		results = res
	}
	end := time.Now()

	analysis := report.Analyze(results, cfg)
	summary := report.Summary{Results: results, Config: cfg, Profiles: profiles, StartedAt: start, EndedAt: end}

	if err := report.WriteCSV(cfg.OutDir, results); err != nil {
		fmt.Fprintln(os.Stderr, "report error:", err)
	}
	if err := report.WriteSummaryMarkdown(cfg.OutDir, summary, analysis); err != nil {
		fmt.Fprintln(os.Stderr, "report error:", err)
	}
	if err := report.WriteFullLogJSONL(cfg.OutDir, results); err != nil {
		fmt.Fprintln(os.Stderr, "report error:", err)
	}
	if cfg.JSON {
		if err := report.WriteSummaryJSON(cfg.OutDir, summary, analysis); err != nil {
			fmt.Fprintln(os.Stderr, "report error:", err)
		}
	}

	// Optional Codex review step
	codexResult := maybeRunCodexReview(cfg, cfg.OutDir)
	if codexResult != nil {
		if codexResult.Status != tests.StatusPass {
			fmt.Fprintf(os.Stderr, "codex review: %s\n", codexResult.ErrorMessage)
		}
	}

	if cfg.Suite.Analysis.Enabled && cfg.Suite.Analysis.FailOnFlaky && len(flakyStatsForMode(analysis, cfg.Suite.Mode)) > 0 {
		return 1
	}
	if hasFailures(results) {
		pass, fail, timeout, unsup := countStatuses(results)
		fmt.Fprintf(os.Stderr, "Run completed with failures: P%d F%d T%d U%d. See %s/summary.md\n", pass, fail, timeout, unsup, cfg.OutDir)
		return 1
	}
	return 0
}

func selectProfiles(profiles []config.ModelProfile, name string) ([]config.ModelProfile, error) {
	if name == "" {
		return profiles, nil
	}
	for _, p := range profiles {
		if p.Name == name {
			return []config.ModelProfile{p}, nil
		}
	}
	return nil, fmt.Errorf("profile %s not found", name)
}

func hasFailures(results []tests.Result) bool {
	for _, r := range results {
		if r.IsWarmup {
			continue
		}
		if r.Status == tests.StatusFail || r.Status == tests.StatusTimeout {
			return true
		}
	}
	return false
}

func flakyStatsForMode(analysis report.Analysis, mode string) []report.TestStats {
	switch strings.TrimSpace(mode) {
	case config.ModeStrict:
		return analysis.Spec.Flaky
	default:
		return analysis.Compatibility.Flaky
	}
}

func countStatuses(results []tests.Result) (pass, fail, timeout, unsupported int) {
	for _, r := range results {
		if r.IsWarmup {
			continue
		}
		switch r.Status {
		case tests.StatusPass:
			pass++
		case tests.StatusFail:
			fail++
		case tests.StatusTimeout:
			timeout++
		case tests.StatusUnsupported:
			unsupported++
		}
	}
	return pass, fail, timeout, unsupported
}

func filterTests(cfg config.Config, all []tests.TestCase, selected map[string]struct{}) []tests.TestCase {
	filtered := make([]tests.TestCase, 0, len(all))
	for _, t := range all {
		if len(selected) > 0 {
			if _, ok := selected[t.ID]; !ok {
				continue
			}
		}
		if ov, ok := cfg.Suite.Tests[t.ID]; ok && ov.Enabled != nil && !*ov.Enabled {
			continue
		}
		if t.RequiresStream && !cfg.Suite.Stream.Enabled {
			continue
		}
		if t.RequiresTools && !cfg.Suite.ToolCalling.Enabled {
			continue
		}
		if t.RequiresStructured && !cfg.Suite.StructuredOutputs.Enabled {
			continue
		}
		if t.RequiresConversations && !cfg.Suite.Conversations.Enabled {
			continue
		}
		if t.RequiresMemory && !cfg.Suite.Memory.Enabled {
			continue
		}
		filtered = append(filtered, t)
	}
	return filtered
}

func collectSelectedTestIDs(rawList string, rawCSVPath string) (map[string]struct{}, error) {
	selected := map[string]struct{}{}
	for _, item := range strings.Split(rawList, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		selected[item] = struct{}{}
	}
	if strings.TrimSpace(rawCSVPath) == "" {
		if len(selected) == 0 {
			return nil, nil
		}
		return selected, nil
	}
	failed, err := loadFailedTestIDs(rawCSVPath)
	if err != nil {
		return nil, err
	}
	for _, id := range failed {
		selected[id] = struct{}{}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no FAIL/TIMEOUT tests found in %s", rawCSVPath)
	}
	return selected, nil
}

func loadFailedTestIDs(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("empty csv: %s", path)
	}
	header := rows[0]
	testIdx := indexOfCSVColumn(header, "test_id")
	statusIdx := indexOfCSVColumn(header, "status")
	warmupIdx := indexOfCSVColumn(header, "warmup")
	if testIdx < 0 || statusIdx < 0 {
		return nil, fmt.Errorf("csv missing required columns test_id/status: %s", path)
	}

	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, row := range rows[1:] {
		if testIdx >= len(row) || statusIdx >= len(row) {
			continue
		}
		if warmupIdx >= 0 && warmupIdx < len(row) && strings.EqualFold(strings.TrimSpace(row[warmupIdx]), "true") {
			continue
		}
		status := strings.TrimSpace(row[statusIdx])
		if status != string(tests.StatusFail) && status != string(tests.StatusTimeout) {
			continue
		}
		testID := strings.TrimSpace(row[testIdx])
		if testID == "" {
			continue
		}
		if _, ok := seen[testID]; ok {
			continue
		}
		seen[testID] = struct{}{}
		out = append(out, testID)
	}
	sort.Strings(out)
	return out, nil
}

func indexOfCSVColumn(header []string, want string) int {
	for i, col := range header {
		if strings.TrimSpace(col) == want {
			return i
		}
	}
	return -1
}

func testIDsForDisplay(list []tests.TestCase) []string {
	out := make([]string, 0, len(list))
	for _, t := range list {
		out = append(out, t.ID)
	}
	return out
}

func maybeRunCodexReview(cfg config.Config, outDir string) *tests.Result {
	if !cfg.Suite.CodexReview.Enabled {
		return nil
	}
	result := tests.Result{
		TestID:   "codex.review",
		TestName: "Codex review",
		Profile:  "system",
		Pass:     1,
		Model:    cfg.Suite.CodexReview.Model,
		Status:   tests.StatusFail,
	}
	if _, err := exec.LookPath(cfg.Suite.CodexReview.CodexBin); err != nil {
		result.Status = tests.StatusUnsupported
		result.ErrorType = "missing_binary"
		result.ErrorMessage = "codex binary not found"
		return &result
	}

	prompt := cfg.Suite.CodexReview.PromptTemplate + "\n\nReports:\n- " + filepath.Join(outDir, "summary.md") + "\n- " + filepath.Join(outDir, "raw.csv") + "\n- " + filepath.Join(outDir, "full_log.jsonl") + "\n"
	args := []string{"--ask-for-approval", cfg.Suite.CodexReview.Approvals, "exec", "--skip-git-repo-check", "--model", cfg.Suite.CodexReview.Model, "-c", "model_reasoning_effort=" + cfg.Suite.CodexReview.ReasoningEffort, "--sandbox", cfg.Suite.CodexReview.Sandbox}
	cmd := exec.Command(cfg.Suite.CodexReview.CodexBin, args...)
	cmd.Stdin = strings.NewReader(prompt)
	out, err := cmd.CombinedOutput()
	if err != nil {
		result.Status = tests.StatusFail
		result.ErrorType = "codex_error"
		result.ErrorMessage = err.Error()
		if len(out) == 0 {
			out = []byte("codex review failed: " + err.Error() + "\n")
		}
	} else {
		result.Status = tests.StatusPass
	}
	_ = os.MkdirAll(outDir, 0o755)
	_ = os.WriteFile(filepath.Join(outDir, "codex_review.md"), out, 0o644)
	return &result
}

func resolveRunOutDir(baseDir string, baseURL string, profiles []config.ModelProfile, startedAt time.Time) string {
	if strings.TrimSpace(baseDir) == "" {
		baseDir = "reports"
	}
	baseDir = filepath.Clean(baseDir)
	timestamp := startedAt.Format("20060102_150405")
	if filepath.Base(baseDir) != "reports" {
		return baseDir + "_" + timestamp
	}
	name := strings.Join([]string{
		runProfileSegment(baseURL, profiles),
		timestamp,
	}, "_")
	return filepath.Join(baseDir, name)
}

func runProfileSegment(baseURL string, profiles []config.ModelProfile) string {
	if len(profiles) != 1 {
		return "multi"
	}
	p := profiles[0]
	switch {
	case strings.TrimSpace(p.Name) != "":
		return sanitizeRunSegment(p.Name)
	case strings.TrimSpace(p.ResponsesModel) != "" && p.ResponsesModel == p.ChatModel:
		return sanitizeRunSegment(p.ResponsesModel)
	case strings.TrimSpace(p.ResponsesModel) != "":
		return sanitizeRunSegment(p.ResponsesModel)
	case strings.TrimSpace(p.ChatModel) != "":
		return sanitizeRunSegment(p.ChatModel)
	case strings.TrimSpace(baseURL) != "":
		return sanitizeRunSegment(baseURL)
	default:
		return "unknown"
	}
}

func sanitizeRunSegment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range s {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}
