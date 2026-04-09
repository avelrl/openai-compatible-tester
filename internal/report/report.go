package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/avelrl/openai-compatible-tester/internal/config"
	"github.com/avelrl/openai-compatible-tester/internal/tests"
)

const ToolVersion = "0.1.0"

type Summary struct {
	Results   []tests.Result
	Config    config.Config
	Profiles  []config.ModelProfile
	StartedAt time.Time
	EndedAt   time.Time
}

type Analysis struct {
	Spec          SpecAnalysis          `json:"spec"`
	Compatibility CompatibilityAnalysis `json:"compatibility"`
}

type SpecAnalysis struct {
	SurfaceReadiness []AgentReadiness   `json:"surface_readiness"`
	Stats            []TestStats        `json:"stats"`
	Unsupported      []Incompatibility  `json:"unsupported"`
	Violations       []Incompatibility  `json:"violations"`
}

type CompatibilityAnalysis struct {
	AgentReady     []AgentReadiness      `json:"agent_readiness"`
	ClientCompat   []ClientCompatibility `json:"client_compatibility"`
	BasicExactness []BasicExactness      `json:"basic_exactness"`
	Stats          []TestStats           `json:"stats"`
	Flaky          []TestStats           `json:"flaky"`
	Unsupported    []Incompatibility     `json:"unsupported"`
	Incompat       []Incompatibility     `json:"incompat"`
	SanitySkips    []SanitySkip          `json:"sanity_skips"`
}

type AgentReadiness struct {
	Profile string   `json:"profile"`
	Surface string   `json:"surface"`
	Verdict string   `json:"verdict"`
	Ready   bool     `json:"ready"`
	Reasons []string `json:"reasons,omitempty"`
	Notes   []string `json:"notes,omitempty"`
}

type BasicExactness struct {
	Profile            string `json:"profile"`
	Surface            string `json:"surface"`
	ProtocolCompatible bool   `json:"protocol_compatible"`
	ExactMatch         bool   `json:"exact_match"`
	Details            string `json:"details,omitempty"`
}

type ClientCompatibility struct {
	Profile        string   `json:"profile"`
	TargetID       string   `json:"target_id"`
	TargetName     string   `json:"target_name"`
	Category       string   `json:"category"`
	DocsURL        string   `json:"docs_url,omitempty"`
	ModeName       string   `json:"mode_name"`
	API            string   `json:"api"`
	VerifiedOn     string   `json:"verified_on,omitempty"`
	Source         string   `json:"source,omitempty"`
	Confidence     string   `json:"confidence,omitempty"`
	Verdict        string   `json:"verdict"`
	RequiredPassed int      `json:"required_passed"`
	RequiredTotal  int      `json:"required_total"`
	Reasons        []string `json:"reasons,omitempty"`
	Notes          []string `json:"notes,omitempty"`
}

type TestStats struct {
	TestID      string
	TestName    string
	Profile     string
	Passes      int
	Fails       int
	Unsupported int
	Timeouts    int
	Total       int
	PassRate    float64
	AvgLatency  float64
	Percentiles map[int]float64
}

type Incompatibility struct {
	ErrorType string
	Message   string
	Count     int
	Tests     []string
}

type SanitySkip struct {
	Profile string
	Pass    int
	Count   int
}

type FullLogRecord struct {
	Index                int               `json:"index"`
	TestID               string            `json:"test_id"`
	TestName             string            `json:"test_name"`
	Profile              string            `json:"profile"`
	RunIndex             int               `json:"run_index"`
	Attempts             int               `json:"attempts"`
	Model                string            `json:"model"`
	Status               tests.Status      `json:"status"`
	CompatStatus         tests.Status      `json:"compat_status,omitempty"`
	SpecStatus           tests.Status      `json:"spec_status,omitempty"`
	HTTPStatus           int               `json:"http_status"`
	LatencyMS            int64             `json:"latency_ms"`
	BytesIn              int64             `json:"bytes_in"`
	BytesOut             int64             `json:"bytes_out"`
	Tokens               int               `json:"tokens"`
	ErrorType            string            `json:"error_type,omitempty"`
	ErrorMessage         string            `json:"error_message,omitempty"`
	CompatErrorType      string            `json:"compat_error_type,omitempty"`
	CompatErrorMessage   string            `json:"compat_error_message,omitempty"`
	SpecErrorType        string            `json:"spec_error_type,omitempty"`
	SpecErrorMessage     string            `json:"spec_error_message,omitempty"`
	ToolChoiceMode       string            `json:"tool_choice_mode,omitempty"`
	EffectiveToolChoice  string            `json:"effective_tool_choice,omitempty"`
	ToolChoiceFallback   bool              `json:"tool_choice_fallback_applied"`
	ReasoningEffort      string            `json:"reasoning_effort,omitempty"`
	LiteLLMTimeout       string            `json:"litellm_timeout,omitempty"`
	FunctionCallObserved bool              `json:"function_call_observed"`
	IsWarmup             bool              `json:"warmup"`
	Evidence             *tests.Evidence   `json:"evidence,omitempty"`
	Steps                []tests.TraceStep `json:"steps,omitempty"`
}

func WriteCSV(outDir string, results []tests.Result) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(outDir, "raw.csv"))
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	header := []string{"profile", "run_index", "attempts", "test_id", "test_name", "status", "http_status", "latency_ms", "bytes_in", "bytes_out", "tokens", "error_type", "error_message", "model", "warmup", "tool_choice_mode", "effective_tool_choice", "tool_choice_fallback_applied", "reasoning_effort", "litellm_timeout", "function_call_observed"}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, r := range results {
		rec := []string{
			r.Profile,
			fmt.Sprintf("%d", r.Pass),
			fmt.Sprintf("%d", r.Attempts),
			r.TestID,
			r.TestName,
			string(r.Status),
			fmt.Sprintf("%d", r.HTTPStatus),
			fmt.Sprintf("%d", r.LatencyMS),
			fmt.Sprintf("%d", r.BytesIn),
			fmt.Sprintf("%d", r.BytesOut),
			fmt.Sprintf("%d", r.Tokens),
			r.ErrorType,
			r.ErrorMessage,
			r.Model,
			fmt.Sprintf("%t", r.IsWarmup),
			r.ToolChoiceMode,
			r.EffectiveToolChoice,
			fmt.Sprintf("%t", r.ToolChoiceFallback),
			r.ReasoningEffort,
			r.LiteLLMTimeout,
			fmt.Sprintf("%t", r.FunctionCallObserved),
		}
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	return w.Error()
}

func WriteFullLogJSONL(outDir string, results []tests.Result) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(outDir, "full_log.jsonl"))
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for i, r := range results {
		rec := FullLogRecord{
			Index:                i + 1,
			TestID:               r.TestID,
			TestName:             r.TestName,
			Profile:              r.Profile,
			RunIndex:             r.Pass,
			Attempts:             r.Attempts,
			Model:                r.Model,
			Status:               r.Status,
			CompatStatus:         r.CompatStatus,
			SpecStatus:           r.SpecStatus,
			HTTPStatus:           r.HTTPStatus,
			LatencyMS:            r.LatencyMS,
			BytesIn:              r.BytesIn,
			BytesOut:             r.BytesOut,
			Tokens:               r.Tokens,
			ErrorType:            r.ErrorType,
			ErrorMessage:         r.ErrorMessage,
			CompatErrorType:      r.CompatErrorType,
			CompatErrorMessage:   r.CompatErrorMessage,
			SpecErrorType:        r.SpecErrorType,
			SpecErrorMessage:     r.SpecErrorMessage,
			ToolChoiceMode:       r.ToolChoiceMode,
			EffectiveToolChoice:  r.EffectiveToolChoice,
			ToolChoiceFallback:   r.ToolChoiceFallback,
			ReasoningEffort:      r.ReasoningEffort,
			LiteLLMTimeout:       r.LiteLLMTimeout,
			FunctionCallObserved: r.FunctionCallObserved,
			IsWarmup:             r.IsWarmup,
			Evidence:             r.Evidence,
			Steps:                tests.EffectiveTraceSteps(r),
		}
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}
	return nil
}

func WriteSummaryMarkdown(outDir string, summary Summary, analysis Analysis) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(outDir, "summary.md")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, "# OpenAI Compatibility Test Summary\n\n")
	fmt.Fprintf(f, "**Environment**\n\n")
	fmt.Fprintf(f, "- Base URL: %s\n", summary.Config.BaseURL)
	fmt.Fprintf(f, "- Primary mode: %s\n", summary.Config.Suite.Mode)
	fmt.Fprintf(f, "- Date: %s\n", summary.EndedAt.Format(time.RFC3339))
	fmt.Fprintf(f, "- Version: %s\n\n", ToolVersion)

	compatSummary := summaryForMode(summary, config.ModeCompat)

	fmt.Fprintf(f, "**OpenAI spec conformance**\n\n")
	fmt.Fprintf(f, "_Surface readiness_\n\n")
	writeAgentReadiness(f, analysis.Spec.SurfaceReadiness)
	fmt.Fprintf(f, "_Stats_\n\n")
	writeStats(f, analysis.Spec.Stats)
	fmt.Fprintf(f, "\n_Supported/unsupported surface gaps_\n\n")
	writeUnsupported(f, analysis.Spec.Unsupported)
	fmt.Fprintf(f, "\n_Top spec violations_\n\n")
	writeIncompat(f, analysis.Spec.Violations, analysis.Spec.Unsupported)

	fmt.Fprintf(f, "\n**Compatibility**\n\n")

	fmt.Fprintf(f, "_Agent readiness_\n\n")
	writeAgentReadiness(f, analysis.Compatibility.AgentReady)

	fmt.Fprintf(f, "_Known client compatibility_\n\n")
	writeClientCompatibility(f, analysis.Compatibility.ClientCompat)

	fmt.Fprintf(f, "_Basic text exactness_\n\n")
	writeBasicExactness(f, analysis.Compatibility.BasicExactness)

	fmt.Fprintf(f, "_Results Matrix_\n\n")
	writeMatrix(f, compatSummary)

	fmt.Fprintf(f, "\n_Tool calling stability notes_\n\n")
	writeToolCallingNotes(f, compatSummary)

	fmt.Fprintf(f, "\n_Stats_\n\n")
	writeStats(f, analysis.Compatibility.Stats)

	fmt.Fprintf(f, "\n_Flakiness_\n\n")
	writeFlakiness(f, analysis.Compatibility.Flaky)

	fmt.Fprintf(f, "\n_Sanity skips_\n\n")
	writeSanitySkips(f, analysis.Compatibility.SanitySkips)

	fmt.Fprintf(f, "\n_Unsupported features_\n\n")
	writeUnsupported(f, analysis.Compatibility.Unsupported)

	fmt.Fprintf(f, "\n_Top incompatibilities_\n\n")
	writeIncompat(f, analysis.Compatibility.Incompat, analysis.Compatibility.Unsupported)
	return nil
}

func WriteSummaryJSON(outDir string, summary Summary, analysis Analysis) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(outDir, "summary.json")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	payload := map[string]interface{}{
		"base_url":     summary.Config.BaseURL,
		"version":      ToolVersion,
		"started_at":   summary.StartedAt.Format(time.RFC3339),
		"ended_at":     summary.EndedAt.Format(time.RFC3339),
		"primary_mode": summary.Config.Suite.Mode,
		"results":      summary.Results,
		"analysis":     analysis,
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func Analyze(results []tests.Result, cfg config.Config) Analysis {
	compatResults := projectResults(results, config.ModeCompat)
	strictResults := projectResults(results, config.ModeStrict)

	compatStats := buildStats(compatResults, cfg)
	flaky := make([]TestStats, 0)
	for _, s := range compatStats {
		if s.Total > 1 && s.Passes > 0 && s.PassRate < 1.0 {
			flaky = append(flaky, s)
		}
	}
	compatUnsupported := buildUnsupported(compatResults)
	compatIncompat := buildIncompat(compatResults)
	sanitySkips := buildSanitySkips(compatResults)
	agentReady := buildAgentReadiness(compatResults, cfg)
	clientCompat := buildClientCompatibility(compatResults, cfg)
	basicExactness := buildBasicExactness(compatResults)

	specStats := buildStats(strictResults, cfg)
	specUnsupported := buildUnsupported(strictResults)
	specViolations := buildIncompat(strictResults)
	specReadiness := buildAgentReadiness(strictResults, cfg)

	return Analysis{
		Spec: SpecAnalysis{
			SurfaceReadiness: specReadiness,
			Stats:            specStats,
			Unsupported:      specUnsupported,
			Violations:       specViolations,
		},
		Compatibility: CompatibilityAnalysis{
			AgentReady:     agentReady,
			ClientCompat:   clientCompat,
			BasicExactness: basicExactness,
			Stats:          compatStats,
			Flaky:          flaky,
			Unsupported:    compatUnsupported,
			Incompat:       compatIncompat,
			SanitySkips:    sanitySkips,
		},
	}
}

func projectResults(results []tests.Result, mode string) []tests.Result {
	out := make([]tests.Result, 0, len(results))
	for _, r := range results {
		out = append(out, tests.ProjectForMode(r, mode))
	}
	return out
}

func summaryForMode(summary Summary, mode string) Summary {
	s := summary
	s.Results = projectResults(summary.Results, mode)
	return s
}

func buildAgentReadiness(results []tests.Result, cfg config.Config) []AgentReadiness {
	type testBucket map[string][]tests.Result
	byProfile := map[string]testBucket{}
	for _, r := range results {
		if r.IsWarmup || r.Profile == "system" {
			continue
		}
		if byProfile[r.Profile] == nil {
			byProfile[r.Profile] = testBucket{}
		}
		byProfile[r.Profile][r.TestID] = append(byProfile[r.Profile][r.TestID], r)
	}

	profiles := make([]string, 0, len(byProfile))
	for profile := range byProfile {
		profiles = append(profiles, profile)
	}
	sort.Strings(profiles)

	out := make([]AgentReadiness, 0, len(profiles))
	for _, profile := range profiles {
		testsByID := byProfile[profile]
		out = append(out, buildSurfaceReadiness(profile, "chat", testsByID, cfg))
		out = append(out, buildSurfaceReadiness(profile, "responses", testsByID, cfg))
	}
	return out
}

func buildSurfaceReadiness(profile, surface string, testsByID map[string][]tests.Result, cfg config.Config) AgentReadiness {
	reasons := make([]string, 0)
	notes := make([]string, 0)

	if !anyPass(testsByID, "sanity.models") {
		notes = append(notes, "models listing failed")
	}

	basicID := surface + ".basic"
	if !anyProtocolCompatibleBasic(testsByID, basicID) {
		reasons = append(reasons, fmt.Sprintf("no protocol-compatible basic text path via %s", surface))
	} else if hasExactnessOnlyFailure(testsByID, basicID) {
		notes = append(notes, "basic text path is protocol-compatible, but exact-match obedience is weak")
	}

	if cfg.Suite.Stream.Enabled && !anyPass(testsByID, surface+".stream") {
		reasons = append(reasons, fmt.Sprintf("no successful streaming path via %s", surface))
	}
	if cfg.Suite.ToolCalling.Enabled && !anyPass(testsByID, surface+".tool_call.required") {
		reasons = append(reasons, fmt.Sprintf("no successful required tool-calling path via %s", surface))
	}
	if hasSurfaceSpecViolation(surface, testsByID) {
		notes = append(notes, "non-core surface checks exposed spec violations")
	}

	switch surface {
	case "chat":
		if hasStatus(testsByID, tests.StatusUnsupported, "chat.tool_call") {
			notes = append(notes, "forced tool_choice object is unsupported; use tool_choice=\"required\" or auto")
		}
		if hasStatus(testsByID, tests.StatusUnsupported, "chat.structured.json_object") {
			notes = append(notes, "json_object is unsupported; use json_schema")
		}
		if hasStatus(testsByID, tests.StatusFail, "chat.memory") {
			notes = append(notes, "multi-turn chat memory is unreliable")
		}
	case "responses":
		if anyPass(testsByID, "responses.tool_call") && !anyPass(testsByID, "responses.tool_call.required") {
			notes = append(notes, "function tools work only with tool_choice=\"auto\"")
		}
		if hasStatus(testsByID, tests.StatusFail, "responses.memory.prev_id") {
			notes = append(notes, "previous_response_id follow-up is unreliable")
		}
		if hasStatus(testsByID, tests.StatusFail, "responses.structured.json_schema", "responses.structured.json_object") {
			notes = append(notes, "structured outputs are unreliable")
		}
		if hasStatus(testsByID, tests.StatusUnsupported, "responses.tool_call") {
			notes = append(notes, "forced tool_choice object is unsupported; use tool_choice=\"required\" or auto")
		}
		if hasStatus(testsByID, tests.StatusUnsupported, "responses.store_get", "responses.conversations") {
			notes = append(notes, "retrieval/conversations endpoints are unsupported")
		}
	}

	verdict := "READY"
	ready := true
	if len(reasons) > 0 {
		verdict = "NOT READY"
		ready = false
	} else if len(notes) > 0 {
		verdict = "READY WITH LIMITATIONS"
	}

	return AgentReadiness{
		Profile: profile,
		Surface: surface,
		Verdict: verdict,
		Ready:   ready,
		Reasons: reasons,
		Notes:   notes,
	}
}

func hasSurfaceSpecViolation(surface string, testsByID map[string][]tests.Result) bool {
	prefix := surface + "."
	for testID, list := range testsByID {
		if !strings.HasPrefix(testID, prefix) {
			continue
		}
		if testID == surface+".basic" || testID == surface+".stream" || testID == surface+".tool_call.required" {
			continue
		}
		for _, r := range list {
			if r.Status == tests.StatusFail {
				return true
			}
		}
	}
	return false
}

func anyPass(byTest map[string][]tests.Result, ids ...string) bool {
	for _, id := range ids {
		for _, r := range byTest[id] {
			if r.Status == tests.StatusPass {
				return true
			}
		}
	}
	return false
}

func anyProtocolCompatibleBasic(byTest map[string][]tests.Result, id string) bool {
	for _, r := range byTest[id] {
		if r.Status == tests.StatusPass || isExactnessOnlyFailureResult(r) {
			return true
		}
	}
	return false
}

func hasExactnessOnlyFailure(byTest map[string][]tests.Result, id string) bool {
	for _, r := range byTest[id] {
		if isExactnessOnlyFailureResult(r) {
			return true
		}
	}
	return false
}

func isExactnessOnlyFailureResult(r tests.Result) bool {
	return r.Status == tests.StatusFail &&
		r.ErrorType == "assert" &&
		r.HTTPStatus >= 200 &&
		r.HTTPStatus < 300
}

func hasStatus(byTest map[string][]tests.Result, want tests.Status, ids ...string) bool {
	for _, id := range ids {
		for _, r := range byTest[id] {
			if r.Status == want {
				return true
			}
		}
	}
	return false
}

func buildBasicExactness(results []tests.Result) []BasicExactness {
	type testBucket map[string][]tests.Result
	byProfile := map[string]testBucket{}
	for _, r := range results {
		if r.IsWarmup || r.Profile == "system" {
			continue
		}
		if byProfile[r.Profile] == nil {
			byProfile[r.Profile] = testBucket{}
		}
		byProfile[r.Profile][r.TestID] = append(byProfile[r.Profile][r.TestID], r)
	}

	profiles := make([]string, 0, len(byProfile))
	for profile := range byProfile {
		profiles = append(profiles, profile)
	}
	sort.Strings(profiles)

	out := make([]BasicExactness, 0, len(profiles)*2)
	for _, profile := range profiles {
		out = append(out, summarizeBasicExactness(profile, "chat", byProfile[profile]["chat.basic"]))
		out = append(out, summarizeBasicExactness(profile, "responses", byProfile[profile]["responses.basic"]))
	}
	return out
}

func buildClientCompatibility(results []tests.Result, cfg config.Config) []ClientCompatibility {
	if len(cfg.Clients.Targets) == 0 {
		return nil
	}

	type testBucket map[string][]tests.Result
	byProfile := map[string]testBucket{}
	for _, r := range results {
		if r.IsWarmup || r.Profile == "system" {
			continue
		}
		if byProfile[r.Profile] == nil {
			byProfile[r.Profile] = testBucket{}
		}
		byProfile[r.Profile][r.TestID] = append(byProfile[r.Profile][r.TestID], r)
	}

	profiles := make([]string, 0, len(byProfile))
	for profile := range byProfile {
		profiles = append(profiles, profile)
	}
	sort.Strings(profiles)

	out := make([]ClientCompatibility, 0, len(profiles)*len(cfg.Clients.Targets))
	for _, profile := range profiles {
		for _, target := range cfg.Clients.Targets {
			best := evaluateTargetCompatibility(profile, byProfile[profile], target)
			out = append(out, best)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Profile != out[j].Profile {
			return out[i].Profile < out[j].Profile
		}
		if categoryRank(out[i].Category) != categoryRank(out[j].Category) {
			return categoryRank(out[i].Category) < categoryRank(out[j].Category)
		}
		return out[i].TargetName < out[j].TargetName
	})
	return out
}

func evaluateTargetCompatibility(profile string, byTest map[string][]tests.Result, target config.ClientTarget) ClientCompatibility {
	best := ClientCompatibility{
		Profile:    profile,
		TargetID:   target.ID,
		TargetName: target.Name,
		Category:   target.Category,
		DocsURL:    target.DocsURL,
		Verdict:    "NOT COVERED",
	}
	bestRank := verdictRank(best.Verdict)

	for idx, mode := range target.Modes {
		current := evaluateTargetMode(profile, byTest, target, mode)
		currentRank := verdictRank(current.Verdict)
		if currentRank > bestRank ||
			(currentRank == bestRank && current.RequiredPassed > best.RequiredPassed) ||
			(currentRank == bestRank && current.RequiredPassed == best.RequiredPassed && len(current.Reasons)+len(current.Notes) < len(best.Reasons)+len(best.Notes)) ||
			idx == 0 && best.ModeName == "" {
			best = current
			bestRank = currentRank
		}
	}

	if len(target.Notes) > 0 {
		best.Notes = append(best.Notes, target.Notes...)
	}
	best.Notes = unique(best.Notes)
	best.Reasons = unique(best.Reasons)
	if best.Verdict == "COMPATIBLE" && len(best.Notes) > 0 {
		best.Verdict = "COMPATIBLE WITH LIMITATIONS"
	}
	return best
}

func evaluateTargetMode(profile string, byTest map[string][]tests.Result, target config.ClientTarget, mode config.ClientMode) ClientCompatibility {
	out := ClientCompatibility{
		Profile:    profile,
		TargetID:   target.ID,
		TargetName: target.Name,
		Category:   target.Category,
		DocsURL:    target.DocsURL,
		ModeName:   mode.Name,
		API:        mode.API,
		VerifiedOn: mode.VerifiedOn,
		Source:     mode.Source,
		Confidence: mode.Confidence,
		Verdict:    "NOT COVERED",
	}

	reasons := make([]string, 0)
	notes := make([]string, 0)
	requiredPassed := 0
	missingRequired := false
	unsupportedOK := toSet(mode.UnsupportedOK)

	for _, testID := range mode.RequiredTests {
		outcome := summarizeOutcome(byTest[testID])
		switch {
		case !outcome.Seen:
			missingRequired = true
			reasons = append(reasons, fmt.Sprintf("%s not run", testID))
		case outcome.Pass:
			requiredPassed++
			if outcome.Fail || outcome.Timeout {
				notes = append(notes, fmt.Sprintf("%s is flaky", testID))
			}
			if outcome.Unsupported && !unsupportedOK[testID] {
				notes = append(notes, fmt.Sprintf("%s is intermittently unsupported", testID))
			}
		case outcome.Unsupported && unsupportedOK[testID]:
			notes = append(notes, fmt.Sprintf("%s unsupported but acceptable for this client", testID))
		case outcome.Fail || outcome.Timeout:
			reasons = append(reasons, fmt.Sprintf("%s failed", testID))
		case outcome.Unsupported:
			reasons = append(reasons, fmt.Sprintf("%s unsupported", testID))
		default:
			missingRequired = true
			reasons = append(reasons, fmt.Sprintf("%s inconclusive", testID))
		}
	}

	for _, testID := range mode.OptionalTests {
		outcome := summarizeOutcome(byTest[testID])
		if !outcome.Seen {
			continue
		}
		switch {
		case outcome.Pass && (outcome.Fail || outcome.Timeout):
			notes = append(notes, fmt.Sprintf("%s is flaky", testID))
		case outcome.Fail || outcome.Timeout:
			notes = append(notes, fmt.Sprintf("%s failed", testID))
		case outcome.Unsupported && unsupportedOK[testID]:
			notes = append(notes, fmt.Sprintf("%s unsupported but acceptable for this client", testID))
		case outcome.Unsupported:
			notes = append(notes, fmt.Sprintf("%s unsupported", testID))
		}
	}

	for _, note := range mode.Notes {
		notes = append(notes, note)
	}

	out.RequiredPassed = requiredPassed
	out.RequiredTotal = len(mode.RequiredTests)
	out.Reasons = unique(reasons)
	out.Notes = unique(notes)

	switch {
	case missingRequired:
		out.Verdict = "NOT COVERED"
	case len(out.Reasons) > 0:
		out.Verdict = "NOT COMPATIBLE"
	case len(out.Notes) > 0:
		out.Verdict = "COMPATIBLE WITH LIMITATIONS"
	default:
		out.Verdict = "COMPATIBLE"
	}

	return out
}

type outcomeSummary struct {
	Seen        bool
	Pass        bool
	Fail        bool
	Timeout     bool
	Unsupported bool
}

func summarizeOutcome(list []tests.Result) outcomeSummary {
	var out outcomeSummary
	for _, r := range list {
		out.Seen = true
		switch r.Status {
		case tests.StatusPass:
			out.Pass = true
		case tests.StatusFail:
			out.Fail = true
		case tests.StatusTimeout:
			out.Timeout = true
		case tests.StatusUnsupported:
			out.Unsupported = true
		}
	}
	return out
}

func verdictRank(verdict string) int {
	switch verdict {
	case "COMPATIBLE":
		return 4
	case "COMPATIBLE WITH LIMITATIONS":
		return 3
	case "NOT COMPATIBLE":
		return 2
	case "NOT COVERED":
		return 1
	default:
		return 0
	}
}

func categoryRank(category string) int {
	switch category {
	case "coding_agent":
		return 0
	case "assistant_framework":
		return 1
	default:
		return 2
	}
}

func toSet(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		out[item] = true
	}
	return out
}

func summarizeBasicExactness(profile, surface string, list []tests.Result) BasicExactness {
	entry := BasicExactness{Profile: profile, Surface: surface}
	if len(list) == 0 {
		entry.Details = "basic test not run"
		return entry
	}
	entry.ProtocolCompatible = anyProtocolCompatibleBasic(map[string][]tests.Result{"basic": list}, "basic")
	entry.ExactMatch = anyPass(map[string][]tests.Result{"basic": list}, "basic")

	switch {
	case entry.ExactMatch:
		entry.Details = "exact reply contract passed"
	case entry.ProtocolCompatible:
		entry.Details = "response schema/transport worked, but exact reply contract failed"
	default:
		entry.Details = basicFailureDetail(list)
	}
	return entry
}

func basicFailureDetail(list []tests.Result) string {
	for _, r := range list {
		if strings.TrimSpace(r.ErrorMessage) != "" {
			return r.ErrorMessage
		}
		if strings.TrimSpace(r.ErrorType) != "" {
			return r.ErrorType
		}
	}
	return "basic path failed"
}

func buildStats(results []tests.Result, cfg config.Config) []TestStats {
	group := map[string][]tests.Result{}
	for _, r := range results {
		if r.IsWarmup {
			continue
		}
		key := r.TestID + "::" + r.Profile
		group[key] = append(group[key], r)
	}
	stats := make([]TestStats, 0, len(group))
	for _, list := range group {
		if len(list) == 0 {
			continue
		}
		stat := TestStats{TestID: list[0].TestID, TestName: list[0].TestName, Profile: list[0].Profile, Percentiles: map[int]float64{}}
		latencies := make([]int64, 0, len(list))
		for _, r := range list {
			stat.Total++
			if r.Status == tests.StatusPass {
				stat.Passes++
			}
			if r.Status == tests.StatusFail {
				stat.Fails++
			}
			if r.Status == tests.StatusTimeout {
				stat.Timeouts++
			}
			if r.Status == tests.StatusUnsupported {
				stat.Unsupported++
			}
			if r.LatencyMS > 0 {
				latencies = append(latencies, r.LatencyMS)
			}
		}
		if stat.Total > 0 {
			stat.PassRate = float64(stat.Passes) / float64(stat.Total)
		}
		if len(latencies) > 0 {
			stat.AvgLatency = averageLatency(latencies)
			if cfg.Suite.Analysis.ComputePercentiles {
				for _, p := range cfg.Suite.Analysis.Percentiles {
					stat.Percentiles[p] = percentile(latencies, p)
				}
			}
		}
		stats = append(stats, stat)
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].TestID == stats[j].TestID {
			return stats[i].Profile < stats[j].Profile
		}
		return stats[i].TestID < stats[j].TestID
	})
	return stats
}

func buildIncompat(results []tests.Result) []Incompatibility {
	group := map[string]*Incompatibility{}
	for _, r := range results {
		if r.IsWarmup || r.Status != tests.StatusFail {
			continue
		}
		errorType := r.ErrorType
		if isBasicTextTest(r.TestID) && isExactnessOnlyFailureResult(r) {
			errorType = "exactness"
		}
		key := errorType + "::" + r.ErrorMessage
		entry := group[key]
		if entry == nil {
			entry = &Incompatibility{ErrorType: errorType, Message: r.ErrorMessage}
			group[key] = entry
		}
		entry.Count++
		entry.Tests = append(entry.Tests, r.TestID)
	}
	out := make([]Incompatibility, 0, len(group))
	for _, v := range group {
		v.Tests = unique(v.Tests)
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

func buildUnsupported(results []tests.Result) []Incompatibility {
	group := map[string]*Incompatibility{}
	for _, r := range results {
		if r.IsWarmup || r.Status != tests.StatusUnsupported || r.ErrorType == "sanity_failed" {
			continue
		}
		key := r.ErrorType + "::" + r.ErrorMessage
		entry := group[key]
		if entry == nil {
			entry = &Incompatibility{ErrorType: r.ErrorType, Message: r.ErrorMessage}
			group[key] = entry
		}
		entry.Count++
		entry.Tests = append(entry.Tests, r.TestID)
	}
	out := make([]Incompatibility, 0, len(group))
	for _, v := range group {
		v.Tests = unique(v.Tests)
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

func isBasicTextTest(testID string) bool {
	return testID == "chat.basic" || testID == "responses.basic"
}

func buildSanitySkips(results []tests.Result) []SanitySkip {
	group := map[string]int{}
	for _, r := range results {
		if r.IsWarmup {
			continue
		}
		if r.Status != tests.StatusUnsupported || r.ErrorType != "sanity_failed" {
			continue
		}
		key := fmt.Sprintf("%s::%d", r.Profile, r.Pass)
		group[key]++
	}
	out := make([]SanitySkip, 0, len(group))
	for key, count := range group {
		parts := strings.SplitN(key, "::", 2)
		pass := 0
		if len(parts) == 2 {
			fmt.Sscanf(parts[1], "%d", &pass)
		}
		out = append(out, SanitySkip{Profile: parts[0], Pass: pass, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Profile == out[j].Profile {
			return out[i].Pass < out[j].Pass
		}
		return out[i].Profile < out[j].Profile
	})
	return out
}

func writeMatrix(f *os.File, summary Summary) {
	// header
	profiles := make([]string, 0, len(summary.Profiles))
	for _, p := range summary.Profiles {
		profiles = append(profiles, p.Name)
	}
	fmt.Fprintf(f, "| Test | %s |\n", strings.Join(profiles, " | "))
	fmt.Fprintf(f, "| %s |\n", strings.Join(makeSeparators(len(profiles)+1), " | "))
	// aggregate
	byTest := map[string]map[string][]tests.Result{}
	for _, r := range summary.Results {
		if r.IsWarmup {
			continue
		}
		if byTest[r.TestID] == nil {
			byTest[r.TestID] = map[string][]tests.Result{}
		}
		byTest[r.TestID][r.Profile] = append(byTest[r.TestID][r.Profile], r)
	}
	keys := make([]string, 0, len(byTest))
	for k := range byTest {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, testID := range keys {
		row := []string{testID}
		for _, profile := range profiles {
			list := byTest[testID][profile]
			p, fl, u, t := countStatus(list)
			row = append(row, formatStatusCell(p, fl, u, t))
		}
		fmt.Fprintf(f, "| %s |\n", strings.Join(row, " | "))
	}
}

func writeAgentReadiness(f *os.File, entries []AgentReadiness) {
	if len(entries) == 0 {
		fmt.Fprintln(f, "No agent-readiness data available.")
		fmt.Fprintln(f)
		return
	}
	fmt.Fprintln(f, "| Profile | Surface | Verdict | Details |")
	fmt.Fprintln(f, "| --- | --- | --- | --- |")
	for _, entry := range entries {
		detailParts := append([]string(nil), entry.Reasons...)
		detailParts = append(detailParts, entry.Notes...)
		details := "core agent paths passed"
		if len(detailParts) > 0 {
			details = strings.Join(detailParts, "; ")
		}
		fmt.Fprintf(f, "| %s | %s | %s | %s |\n", entry.Profile, entry.Surface, entry.Verdict, details)
	}
	fmt.Fprintln(f)
}

func writeClientCompatibility(f *os.File, entries []ClientCompatibility) {
	if len(entries) == 0 {
		fmt.Fprintln(f, "No known-client compatibility data configured.")
		fmt.Fprintln(f)
		return
	}
	fmt.Fprintln(f, "| Profile | Category | Client | Best mode | Verification | Verdict | Required | Details |")
	fmt.Fprintln(f, "| --- | --- | --- | --- | --- | --- | --- | --- |")
	for _, entry := range entries {
		name := entry.TargetName
		if strings.TrimSpace(entry.DocsURL) != "" {
			name = fmt.Sprintf("[%s](%s)", entry.TargetName, entry.DocsURL)
		}
		verification := formatVerification(entry)
		details := "all required checks passed"
		if len(entry.Reasons) > 0 {
			details = strings.Join(entry.Reasons, "; ")
		} else if len(entry.Notes) > 0 {
			details = strings.Join(entry.Notes, "; ")
		}
		required := fmt.Sprintf("%d/%d", entry.RequiredPassed, entry.RequiredTotal)
		fmt.Fprintf(f, "| %s | %s | %s | %s (%s) | %s | %s | %s | %s |\n",
			entry.Profile, entry.Category, name, entry.ModeName, entry.API, verification, entry.Verdict, required, details)
	}
	fmt.Fprintln(f)
}

func formatVerification(entry ClientCompatibility) string {
	parts := make([]string, 0, 3)
	if strings.TrimSpace(entry.Source) != "" {
		parts = append(parts, entry.Source)
	}
	if strings.TrimSpace(entry.Confidence) != "" {
		parts = append(parts, entry.Confidence)
	}
	if strings.TrimSpace(entry.VerifiedOn) != "" {
		parts = append(parts, entry.VerifiedOn)
	}
	if len(parts) == 0 {
		return "unspecified"
	}
	return strings.Join(parts, " / ")
}

func writeBasicExactness(f *os.File, entries []BasicExactness) {
	if len(entries) == 0 {
		fmt.Fprintln(f, "No basic exactness data available.")
		fmt.Fprintln(f)
		return
	}
	fmt.Fprintln(f, "| Profile | Surface | Protocol Path | Exact Match | Details |")
	fmt.Fprintln(f, "| --- | --- | --- | --- | --- |")
	for _, entry := range entries {
		protocol := "no"
		exact := "no"
		if entry.ProtocolCompatible {
			protocol = "yes"
		}
		if entry.ExactMatch {
			exact = "yes"
		}
		fmt.Fprintf(f, "| %s | %s | %s | %s | %s |\n", entry.Profile, entry.Surface, protocol, exact, entry.Details)
	}
	fmt.Fprintln(f)
}

func makeSeparators(count int) []string {
	parts := make([]string, 0, count)
	for i := 0; i < count; i++ {
		parts = append(parts, "---")
	}
	return parts
}

func writeStats(f *os.File, stats []TestStats) {
	if len(stats) == 0 {
		fmt.Fprintln(f, "No stats available.")
		return
	}
	fmt.Fprintln(f, "| Test | Profile | Pass rate | Avg latency (ms) | p50 | p95 | p99 |")
	fmt.Fprintln(f, "| --- | --- | --- | --- | --- | --- | --- |")
	for _, s := range stats {
		p50 := s.Percentiles[50]
		p95 := s.Percentiles[95]
		p99 := s.Percentiles[99]
		fmt.Fprintf(f, "| %s | %s | %.2f | %.1f | %.1f | %.1f | %.1f |\n", s.TestID, s.Profile, s.PassRate, s.AvgLatency, p50, p95, p99)
	}
}

func writeFlakiness(f *os.File, stats []TestStats) {
	if len(stats) == 0 {
		fmt.Fprintln(f, "No flaky tests detected.")
		return
	}
	for _, s := range stats {
		fmt.Fprintf(f, "- %s [%s]: pass rate %.2f (%d/%d)\n", s.TestID, s.Profile, s.PassRate, s.Passes, s.Total)
	}
}

func writeSanitySkips(f *os.File, skips []SanitySkip) {
	if len(skips) == 0 {
		fmt.Fprintln(f, "No tests skipped after sanity.")
		return
	}
	for _, s := range skips {
		fmt.Fprintf(f, "- %s pass %d: skipped %d tests\n", s.Profile, s.Pass, s.Count)
	}
}

func writeIncompat(f *os.File, incompat []Incompatibility, unsupported []Incompatibility) {
	if len(incompat) == 0 {
		if len(unsupported) > 0 {
			fmt.Fprintln(f, "No failures detected. See Unsupported features above.")
			return
		}
		fmt.Fprintln(f, "No incompatibilities detected.")
		return
	}
	for _, inc := range incompat {
		fmt.Fprintf(f, "- %s (%d): %s (tests: %s)\n", inc.ErrorType, inc.Count, inc.Message, strings.Join(inc.Tests, ", "))
	}
}

func writeUnsupported(f *os.File, unsupported []Incompatibility) {
	if len(unsupported) == 0 {
		fmt.Fprintln(f, "No unsupported features detected.")
		return
	}
	for _, inc := range unsupported {
		fmt.Fprintf(f, "- %s (%d): %s (tests: %s)\n", inc.ErrorType, inc.Count, inc.Message, strings.Join(inc.Tests, ", "))
	}
}

func countStatus(list []tests.Result) (pass, fail, unsup, timeout int) {
	for _, r := range list {
		switch r.Status {
		case tests.StatusPass:
			pass++
		case tests.StatusFail:
			fail++
		case tests.StatusTimeout:
			timeout++
		case tests.StatusUnsupported:
			unsup++
		}
	}
	return
}

func formatStatusCell(pass, fail, unsup, timeout int) string {
	total := pass + fail + unsup + timeout
	if total == 0 {
		return "—"
	}
	parts := make([]string, 0, 4)
	parts = append(parts, statusPart("✅", pass, total)...)
	parts = append(parts, statusPart("❌", fail, total)...)
	parts = append(parts, statusPart("⏱", timeout, total)...)
	parts = append(parts, statusPart("⛔", unsup, total)...)
	return strings.Join(parts, " ")
}

func statusPart(symbol string, count, total int) []string {
	if count <= 0 {
		return nil
	}
	if total == 1 && count == 1 {
		return []string{symbol}
	}
	return []string{fmt.Sprintf("%s%d", symbol, count)}
}

func writeToolCallingNotes(f *os.File, summary Summary) {
	toolTests := []string{
		"responses.tool_call",
		"responses.tool_call.required",
		"responses.custom_tool",
		"chat.tool_call",
		"chat.tool_call.required",
	}

	byTestProfile := map[string]map[string][]tests.Result{}
	for _, r := range summary.Results {
		if r.IsWarmup {
			continue
		}
		if byTestProfile[r.TestID] == nil {
			byTestProfile[r.TestID] = map[string][]tests.Result{}
		}
		byTestProfile[r.TestID][r.Profile] = append(byTestProfile[r.TestID][r.Profile], r)
	}

	fmt.Fprintln(f, "| Test | Profile | P | F | U | T | function_call | requested | effective | fallback | reasoning | x-litellm-timeout |")
	fmt.Fprintln(f, "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |")

	for _, testID := range toolTests {
		for _, p := range summary.Profiles {
			list := byTestProfile[testID][p.Name]
			if len(list) == 0 {
				continue
			}
			pass, fail, unsup, timeout := countStatus(list)
			observed := 0
			requestedChoice := ""
			effectiveChoice := ""
			fallbackTrue := false
			fallbackSeen := false
			reasoning := ""
			timeoutHeader := ""
			for _, r := range list {
				if r.FunctionCallObserved {
					observed++
				}
				if requestedChoice == "" && strings.TrimSpace(r.ToolChoiceMode) != "" {
					requestedChoice = r.ToolChoiceMode
				}
				if effectiveChoice == "" && strings.TrimSpace(r.EffectiveToolChoice) != "" {
					effectiveChoice = r.EffectiveToolChoice
				}
				if strings.TrimSpace(r.EffectiveToolChoice) != "" {
					fallbackSeen = true
				}
				if r.ToolChoiceFallback {
					fallbackSeen = true
					fallbackTrue = true
				}
				if reasoning == "" && strings.TrimSpace(r.ReasoningEffort) != "" {
					reasoning = r.ReasoningEffort
				}
				if timeoutHeader == "" && strings.TrimSpace(r.LiteLLMTimeout) != "" {
					timeoutHeader = r.LiteLLMTimeout
				}
			}
			fallback := ""
			if fallbackSeen {
				if fallbackTrue {
					fallback = "yes"
				} else {
					fallback = "no"
				}
			}
			fmt.Fprintf(f, "| %s | %s | %d | %d | %d | %d | %d/%d | %s | %s | %s | %s | %s |\n",
				testID, p.Name, pass, fail, unsup, timeout, observed, len(list), requestedChoice, effectiveChoice, fallback, reasoning, timeoutHeader)
		}
	}
}

func averageLatency(latencies []int64) float64 {
	var sum int64
	for _, v := range latencies {
		sum += v
	}
	return float64(sum) / float64(len(latencies))
}

func percentile(latencies []int64, p int) float64 {
	sorted := append([]int64(nil), latencies...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(p)/100.0*float64(len(sorted)-1) + 0.5)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return float64(sorted[idx])
}

func unique(in []string) []string {
	set := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := set[v]; ok {
			continue
		}
		set[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
