package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultSuitePath     = "configs/suite.yaml"
	DefaultModelsPath    = "configs/models.yaml"
	DefaultEndpointsPath = "configs/endpoints.yaml"
	DefaultClientsPath   = "configs/clients.yaml"
	ModeCompat           = "compat"
	ModeStrict           = "strict"
	CapabilityStatusSupported   = "supported"
	CapabilityStatusUnsupported = "unsupported"
	CapabilityStatusDisabled    = "disabled"
	CapabilityStatusUnavailable = "unavailable"
)

type Config struct {
	Endpoints EndpointsConfig
	Models    ModelsConfig
	Suite     SuiteConfig
	Clients   ClientsConfig
	Capabilities CapabilitiesConfig

	BaseURL string
	APIKey  string

	Profile         string
	OutDir          string
	JSON            bool
	NoTUI           bool
	NoStream        bool
	AnalyzeOverride *bool
	Verbose         bool
}

type EndpointsConfig struct {
	BaseURL        string            `yaml:"base_url"`
	APIKeyEnv      string            `yaml:"api_key_env"`
	DefaultHeaders map[string]string `yaml:"default_headers"`
	Paths          EndpointsPaths    `yaml:"paths"`
}

type EndpointsPaths struct {
	Models        string `yaml:"models"`
	Chat          string `yaml:"chat"`
	Responses     string `yaml:"responses"`
	Conversations string `yaml:"conversations"`
	DebugCapabilities string `yaml:"debug_capabilities"`
}

type ModelsConfig struct {
	Profiles []ModelProfile `yaml:"profiles"`
}

type CapabilitiesConfig struct {
	Capabilities map[string]CapabilitySpec `yaml:"capabilities"`
}

type CapabilitySpec struct {
	Status string `yaml:"status"`
	Reason string `yaml:"reason"`
}

type ModelProfile struct {
	Name               string                  `yaml:"name"`
	ChatModel          string                  `yaml:"chat_model"`
	ResponsesModel     string                  `yaml:"responses_model"`
	ChatMaxTokensParam string                  `yaml:"chat_max_tokens_param"`
	ReasoningEffort    string                  `yaml:"reasoning_effort"`
	Temperature        *float64                `yaml:"temperature"`
	RateLimitPerMinute int                     `yaml:"rate_limit_per_minute"`
	Extra              map[string]interface{}  `yaml:"extra"`
	Tests              map[string]TestOverride `yaml:"tests"`
}

type SuiteConfig struct {
	Mode              string                  `yaml:"mode"`
	Target            string                  `yaml:"target"`
	Passes            int                     `yaml:"passes"`
	WarmupPasses      int                     `yaml:"warmup_passes"`
	Parallelism       int                     `yaml:"parallelism"`
	TimeoutSeconds    int                     `yaml:"timeout_seconds"`
	Retry             RetryConfig             `yaml:"retry"`
	Report            ReportConfig            `yaml:"report"`
	LiteLLMHeaders    map[string]string       `yaml:"litellm_headers"`
	Stream            Toggle                  `yaml:"stream"`
	ToolCalling       Toggle                  `yaml:"tool_calling"`
	StructuredOutputs Toggle                  `yaml:"structured_outputs"`
	Conversations     Toggle                  `yaml:"conversations"`
	Memory            Toggle                  `yaml:"memory"`
	ChatReasoning     Toggle                  `yaml:"chat_reasoning"`
	Tests             map[string]TestOverride `yaml:"tests"`
	Analysis          AnalysisConfig          `yaml:"analysis"`
	CodexReview       CodexReviewConfig       `yaml:"codex_review"`
}

type ClientsConfig struct {
	Targets []ClientTarget `yaml:"targets"`
}

type ClientTarget struct {
	ID       string       `yaml:"id"`
	Name     string       `yaml:"name"`
	Category string       `yaml:"category"`
	DocsURL  string       `yaml:"docs_url"`
	Notes    []string     `yaml:"notes"`
	Modes    []ClientMode `yaml:"modes"`
}

type ClientMode struct {
	Name          string   `yaml:"name"`
	API           string   `yaml:"api"`
	VerifiedOn    string   `yaml:"verified_on"`
	Source        string   `yaml:"source"`
	Confidence    string   `yaml:"confidence"`
	RequiredTests []string `yaml:"required_tests"`
	OptionalTests []string `yaml:"optional_tests"`
	UnsupportedOK []string `yaml:"unsupported_ok"`
	Notes         []string `yaml:"notes"`
}

type ReportConfig struct {
	SnippetLimitBytes int `yaml:"snippet_limit_bytes"`
}

type RetryConfig struct {
	MaxAttempts          int   `yaml:"max_attempts"`
	BackoffMS            int   `yaml:"backoff_ms"`
	RetryOnStatus        []int `yaml:"retry_on_status"`
	TestRetries          int   `yaml:"test_retries"`
	RateLimitMaxAttempts int   `yaml:"rate_limit_max_attempts"`
	RateLimitFallbackMS  int   `yaml:"rate_limit_fallback_ms"`
}

type Toggle struct {
	Enabled bool `yaml:"enabled"`
}

type TestOverride struct {
	// Enabled can explicitly disable a test regardless of capability toggles.
	Enabled *bool `yaml:"enabled"`

	TimeoutSeconds int `yaml:"timeout_seconds"`

	// Stream controls streaming mode for tests that optionally support it.
	Stream               *bool `yaml:"stream"`
	StreamTimeoutSeconds int   `yaml:"stream_timeout_seconds"`

	// LiteLLM-specific request headers (merged with suite-level LiteLLMHeaders).
	LiteLLMHeaders map[string]string `yaml:"litellm_headers"`

	// InstructionRole overrides the role used for the leading instruction message
	// in chat tests that separate instruction from user input.
	InstructionRole          string `yaml:"instruction_role"` // developer|system|user
	InstructionText          string `yaml:"instruction_text"`
	UserText                 string `yaml:"user_text"`
	MergeInstructionIntoUser *bool  `yaml:"merge_instruction_into_user"`

	// Tool calling knobs (used by tool_call tests).
	ToolChoiceMode    string `yaml:"tool_choice_mode"` // forced|forced_compat|required|auto
	ForcedToolName    string `yaml:"forced_tool_name"`
	ParallelToolCalls *bool  `yaml:"parallel_tool_calls"`
	ReasoningEffort   string `yaml:"reasoning_effort"`  // minimal|low|medium|high|none|omit
	MaxOutputTokens   *int   `yaml:"max_output_tokens"` // Responses
	MaxTokens         *int   `yaml:"max_tokens"`        // Chat
	StrictMode        *bool  `yaml:"strict_mode"`

	// If true (optionally limited to specific profiles), TIMEOUT results are converted to UNSUPPORTED.
	TreatTimeoutAsUnsupported         bool     `yaml:"treat_timeout_as_unsupported"`
	TreatTimeoutAsUnsupportedProfiles []string `yaml:"treat_timeout_as_unsupported_profiles"`
}

type AnalysisConfig struct {
	Enabled            bool  `yaml:"enabled"`
	ComputePercentiles bool  `yaml:"compute_percentiles"`
	Percentiles        []int `yaml:"percentiles"`
	Flakiness          bool  `yaml:"flakiness"`
	FailOnFlaky        bool  `yaml:"fail_on_flaky"`
}

type CodexReviewConfig struct {
	Enabled         bool   `yaml:"enabled"`
	CodexBin        string `yaml:"codex_bin"`
	Model           string `yaml:"model"`
	ReasoningEffort string `yaml:"reasoning_effort"`
	Sandbox         string `yaml:"sandbox"`
	Approvals       string `yaml:"approvals"`
	PromptTemplate  string `yaml:"prompt_template"`
}

type LoadOptions struct {
	SuitePath     string
	ModelsPath    string
	EndpointsPath string
	ClientsPath   string
	CapabilitiesPath string
	EnvFile       string
	Mode          string

	BaseURL    string
	APIKey     string
	Profile    string
	Passes     int
	RetryFails int
	NoTUI      bool
	JSON       bool
	NoStream   bool
	Analyze    *bool
	OutDir     string
	Verbose    bool
}

func Load(opts LoadOptions) (Config, error) {
	suitePath := opts.SuitePath
	if suitePath == "" {
		suitePath = DefaultSuitePath
	}
	modelsPath := opts.ModelsPath
	if modelsPath == "" {
		modelsPath = DefaultModelsPath
	}
	endpointsPath := opts.EndpointsPath
	if endpointsPath == "" {
		endpointsPath = DefaultEndpointsPath
	}
	clientsPath := opts.ClientsPath
	if clientsPath == "" {
		clientsPath = DefaultClientsPath
	}

	var cfg Config

	suite, err := loadYAML[SuiteConfig](suitePath)
	if err != nil {
		return cfg, fmt.Errorf("load suite: %w", err)
	}
	models, err := loadYAML[ModelsConfig](modelsPath)
	if err != nil {
		return cfg, fmt.Errorf("load models: %w", err)
	}
	endpoints, err := loadYAML[EndpointsConfig](endpointsPath)
	if err != nil {
		return cfg, fmt.Errorf("load endpoints: %w", err)
	}
	clients, err := loadOptionalYAML[ClientsConfig](clientsPath)
	if err != nil {
		return cfg, fmt.Errorf("load clients: %w", err)
	}
	var capabilities CapabilitiesConfig
	if strings.TrimSpace(opts.CapabilitiesPath) != "" {
		capabilities, err = loadYAML[CapabilitiesConfig](opts.CapabilitiesPath)
		if err != nil {
			return cfg, fmt.Errorf("load capabilities: %w", err)
		}
		if err := validateCapabilities(capabilities); err != nil {
			return cfg, err
		}
	}

	cfg.Suite = suite
	cfg.Models = models
	cfg.Endpoints = endpoints
	cfg.Clients = clients
	cfg.Capabilities = capabilities

	envMap, err := LoadEnvFiles([]string{".env", opts.EnvFile})
	if err != nil {
		return cfg, err
	}

	// Base URL precedence: YAML < .env < OS env < flag
	baseURL := firstNonEmpty(
		envMap["BASE_URL"],
		envMap["OPENAI_BASE_URL"],
		os.Getenv("BASE_URL"),
		os.Getenv("OPENAI_BASE_URL"),
		cfg.Endpoints.BaseURL,
	)
	if opts.BaseURL != "" {
		baseURL = opts.BaseURL
	}
	cfg.BaseURL = normalizeBaseURL(baseURL)

	// API key from env var name in endpoints.yaml
	apiKeyEnv := strings.TrimSpace(cfg.Endpoints.APIKeyEnv)
	if apiKeyEnv == "" {
		apiKeyEnv = "OPENAI_API_KEY"
		cfg.Endpoints.APIKeyEnv = apiKeyEnv
	}
	apiKey := firstNonEmpty(
		envMap[apiKeyEnv],
		os.Getenv(apiKeyEnv),
	)
	if opts.APIKey != "" {
		apiKey = opts.APIKey
	}
	cfg.APIKey = apiKey

	cfg.Profile = opts.Profile
	cfg.NoTUI = opts.NoTUI
	cfg.JSON = opts.JSON
	cfg.NoStream = opts.NoStream
	cfg.AnalyzeOverride = opts.Analyze
	cfg.Verbose = opts.Verbose
	if strings.TrimSpace(opts.Mode) != "" {
		cfg.Suite.Mode = strings.ToLower(strings.TrimSpace(opts.Mode))
	}

	if opts.Passes > 0 {
		cfg.Suite.Passes = opts.Passes
	}
	if opts.RetryFails >= 0 {
		cfg.Suite.Retry.TestRetries = opts.RetryFails
	}
	if opts.OutDir != "" {
		cfg.OutDir = opts.OutDir
	} else {
		cfg.OutDir = "reports"
	}
	if cfg.NoStream {
		cfg.Suite.Stream.Enabled = false
	}
	if cfg.AnalyzeOverride != nil {
		cfg.Suite.Analysis.Enabled = *cfg.AnalyzeOverride
	}

	applyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func applyDefaults(cfg *Config) {
	if strings.TrimSpace(cfg.Suite.Mode) == "" {
		cfg.Suite.Mode = ModeCompat
	}
	cfg.Suite.Target = strings.TrimSpace(cfg.Suite.Target)
	if cfg.Endpoints.Paths.Models == "" {
		cfg.Endpoints.Paths.Models = "/v1/models"
	}
	if cfg.Endpoints.Paths.Chat == "" {
		cfg.Endpoints.Paths.Chat = "/v1/chat/completions"
	}
	if cfg.Endpoints.Paths.Responses == "" {
		cfg.Endpoints.Paths.Responses = "/v1/responses"
	}
	if cfg.Endpoints.Paths.Conversations == "" {
		cfg.Endpoints.Paths.Conversations = "/v1/conversations"
	}
	if cfg.Endpoints.Paths.DebugCapabilities == "" {
		cfg.Endpoints.Paths.DebugCapabilities = "/debug/capabilities"
	}
	cfg.Endpoints.Paths.Models = ensureLeadingSlash(cfg.Endpoints.Paths.Models)
	cfg.Endpoints.Paths.Chat = ensureLeadingSlash(cfg.Endpoints.Paths.Chat)
	cfg.Endpoints.Paths.Responses = ensureLeadingSlash(cfg.Endpoints.Paths.Responses)
	cfg.Endpoints.Paths.Conversations = ensureLeadingSlash(cfg.Endpoints.Paths.Conversations)
	cfg.Endpoints.Paths.DebugCapabilities = ensureLeadingSlash(cfg.Endpoints.Paths.DebugCapabilities)

	if cfg.Endpoints.DefaultHeaders == nil {
		cfg.Endpoints.DefaultHeaders = map[string]string{}
	}
	if cfg.Suite.LiteLLMHeaders == nil {
		cfg.Suite.LiteLLMHeaders = map[string]string{}
	}
	if cfg.Suite.Tests == nil {
		cfg.Suite.Tests = map[string]TestOverride{}
	}
	if cfg.Clients.Targets == nil {
		cfg.Clients.Targets = []ClientTarget{}
	}
	for k, t := range cfg.Suite.Tests {
		if t.LiteLLMHeaders == nil {
			t.LiteLLMHeaders = map[string]string{}
		}
		cfg.Suite.Tests[k] = t
	}
	if cfg.Capabilities.Capabilities == nil {
		cfg.Capabilities.Capabilities = map[string]CapabilitySpec{}
	}
	if cfg.Suite.Retry.MaxAttempts == 0 {
		cfg.Suite.Retry.MaxAttempts = 1
	}
	if cfg.Suite.Retry.BackoffMS == 0 {
		cfg.Suite.Retry.BackoffMS = 250
	}
	if cfg.Suite.Retry.RateLimitMaxAttempts == 0 {
		cfg.Suite.Retry.RateLimitMaxAttempts = cfg.Suite.Retry.MaxAttempts
	}
	if cfg.Suite.Retry.RateLimitFallbackMS == 0 {
		cfg.Suite.Retry.RateLimitFallbackMS = cfg.Suite.Retry.BackoffMS
	}
	if cfg.Suite.Retry.TestRetries < 0 {
		cfg.Suite.Retry.TestRetries = 0
	}
	if cfg.Suite.Report.SnippetLimitBytes == 0 {
		cfg.Suite.Report.SnippetLimitBytes = 4096
	}
	if cfg.Suite.Parallelism == 0 {
		cfg.Suite.Parallelism = 1
	}
	if cfg.Suite.TimeoutSeconds == 0 {
		cfg.Suite.TimeoutSeconds = 300
	}
}

func (c Config) Validate() error {
	if c.BaseURL == "" {
		return errors.New("base_url is required (via endpoints.yaml, .env, env var, or --base-url)")
	}
	if err := validateCapabilities(c.Capabilities); err != nil {
		return err
	}
	parsedBaseURL, err := url.Parse(c.BaseURL)
	if err != nil {
		return fmt.Errorf("invalid base_url: %w", err)
	}
	for _, endpoint := range []struct {
		name string
		path string
		}{
			{name: "models", path: c.Endpoints.Paths.Models},
			{name: "chat", path: c.Endpoints.Paths.Chat},
			{name: "responses", path: c.Endpoints.Paths.Responses},
			{name: "conversations", path: c.Endpoints.Paths.Conversations},
			{name: "debug_capabilities", path: c.Endpoints.Paths.DebugCapabilities},
		} {
		if hasPathOverlap(parsedBaseURL.Path, endpoint.path) {
			return fmt.Errorf("endpoints.%s path %q overlaps with base_url path %q; remove the duplicated prefix", endpoint.name, endpoint.path, parsedBaseURL.Path)
		}
	}
	if len(c.Models.Profiles) == 0 {
		return errors.New("models.yaml: profiles is required")
	}
	for i := range c.Models.Profiles {
		p := &c.Models.Profiles[i]
		if strings.TrimSpace(p.Name) == "" {
			return errors.New("models.yaml: profile name is required")
		}
		if strings.TrimSpace(p.ChatModel) == "" {
			return fmt.Errorf("models.yaml: chat_model required for profile %s", p.Name)
		}
		if strings.TrimSpace(p.ResponsesModel) == "" {
			return fmt.Errorf("models.yaml: responses_model required for profile %s", p.Name)
		}
		if p.RateLimitPerMinute < 0 {
			return fmt.Errorf("models.yaml: rate_limit_per_minute must be >= 0 for profile %s", p.Name)
		}
		if param := strings.TrimSpace(p.ChatMaxTokensParam); param != "" {
			switch param {
			case "max_tokens", "max_completion_tokens":
			default:
				return fmt.Errorf("models.yaml: chat_max_tokens_param must be max_tokens or max_completion_tokens for profile %s", p.Name)
			}
		}
		if p.Extra == nil {
			p.Extra = map[string]interface{}{}
		}
		for testID, t := range p.Tests {
			if err := validateTestOverride(fmt.Sprintf("models.yaml: profiles.%s.tests.%s", p.Name, testID), testID, t); err != nil {
				return err
			}
		}
	}
	if c.Suite.Passes <= 0 {
		return errors.New("suite.yaml: passes must be > 0")
	}
	switch strings.TrimSpace(c.Suite.Mode) {
	case ModeCompat, ModeStrict:
	default:
		return errors.New("suite.yaml: mode must be compat or strict")
	}
	if c.Suite.WarmupPasses < 0 {
		return errors.New("suite.yaml: warmup_passes must be >= 0")
	}
	if c.Suite.Parallelism <= 0 {
		return errors.New("suite.yaml: parallelism must be > 0")
	}
	if c.Suite.TimeoutSeconds <= 0 {
		return errors.New("suite.yaml: timeout_seconds must be > 0")
	}
	if c.Suite.Retry.MaxAttempts <= 0 {
		return errors.New("suite.yaml: retry.max_attempts must be > 0")
	}
	if c.Suite.Retry.BackoffMS < 0 {
		return errors.New("suite.yaml: retry.backoff_ms must be >= 0")
	}
	if c.Suite.Retry.RateLimitMaxAttempts <= 0 {
		return errors.New("suite.yaml: retry.rate_limit_max_attempts must be > 0")
	}
	if c.Suite.Retry.RateLimitFallbackMS < 0 {
		return errors.New("suite.yaml: retry.rate_limit_fallback_ms must be >= 0")
	}
	if c.Suite.Retry.TestRetries < 0 {
		return errors.New("suite.yaml: retry.test_retries must be >= 0")
	}
	if c.Suite.Report.SnippetLimitBytes < 0 {
		return errors.New("suite.yaml: report.snippet_limit_bytes must be >= 0")
	}
	for testID, t := range c.Suite.Tests {
		if err := validateTestOverride(fmt.Sprintf("suite.yaml: tests.%s", testID), testID, t); err != nil {
			return err
		}
	}
	for _, p := range c.Suite.Analysis.Percentiles {
		if p <= 0 || p >= 100 {
			return fmt.Errorf("analysis.percentiles must be between 1 and 99: %d", p)
		}
	}
	if err := c.Clients.Validate(); err != nil {
		return err
	}
	return nil
}

func (c CapabilitiesConfig) Lookup(name string) (CapabilitySpec, bool) {
	if len(c.Capabilities) == 0 {
		return CapabilitySpec{}, false
	}
	spec, ok := c.Capabilities[strings.TrimSpace(name)]
	if !ok {
		return CapabilitySpec{}, false
	}
	spec.Status = normalizeCapabilityStatus(spec.Status)
	spec.Reason = strings.TrimSpace(spec.Reason)
	return spec, true
}

func validateCapabilities(cfg CapabilitiesConfig) error {
	for key, spec := range cfg.Capabilities {
		name := strings.TrimSpace(key)
		if name == "" {
			return errors.New("capabilities: empty capability key")
		}
		switch normalizeCapabilityStatus(spec.Status) {
		case CapabilityStatusSupported, CapabilityStatusUnsupported, CapabilityStatusDisabled, CapabilityStatusUnavailable:
		default:
			return fmt.Errorf("capabilities.%s.status: unsupported value %q", name, spec.Status)
		}
	}
	return nil
}

func normalizeCapabilityStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

func validateTestOverride(scope, testID string, t TestOverride) error {
	if strings.TrimSpace(testID) == "" {
		return fmt.Errorf("%s: empty test id key", scope)
	}
	if t.TimeoutSeconds < 0 {
		return fmt.Errorf("%s.timeout_seconds must be >= 0", scope)
	}
	if t.StreamTimeoutSeconds < 0 {
		return fmt.Errorf("%s.stream_timeout_seconds must be >= 0", scope)
	}
	if t.MaxOutputTokens != nil && *t.MaxOutputTokens < 0 {
		return fmt.Errorf("%s.max_output_tokens must be >= 0", scope)
	}
	if t.MaxTokens != nil && *t.MaxTokens < 0 {
		return fmt.Errorf("%s.max_tokens must be >= 0", scope)
	}
	if mode := strings.TrimSpace(t.ToolChoiceMode); mode != "" {
		switch mode {
		case "forced", "forced_compat", "required", "auto":
		default:
			return fmt.Errorf("%s.tool_choice_mode must be one of forced|forced_compat|required|auto", scope)
		}
	}
	if role := strings.ToLower(strings.TrimSpace(t.InstructionRole)); role != "" {
		switch role {
		case "developer", "system", "user":
		default:
			return fmt.Errorf("%s.instruction_role must be one of developer|system|user", scope)
		}
	}
	if eff := strings.TrimSpace(t.ReasoningEffort); eff != "" {
		switch eff {
		case "minimal", "low", "medium", "high", "none", "omit":
		default:
			return fmt.Errorf("%s.reasoning_effort must be one of minimal|low|medium|high|none|omit", scope)
		}
	}
	return nil
}

func hasPathOverlap(basePath, endpointPath string) bool {
	baseParts := splitPathSegments(basePath)
	endpointParts := splitPathSegments(endpointPath)
	if len(baseParts) == 0 || len(endpointParts) == 0 {
		return false
	}
	maxOverlap := len(baseParts)
	if len(endpointParts) < maxOverlap {
		maxOverlap = len(endpointParts)
	}
	for overlap := maxOverlap; overlap > 0; overlap-- {
		baseTail := baseParts[len(baseParts)-overlap:]
		endpointHead := endpointParts[:overlap]
		match := true
		for i := 0; i < overlap; i++ {
			if baseTail[i] != endpointHead[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func splitPathSegments(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func loadYAML[T any](path string) (T, error) {
	var out T
	data, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}

func loadOptionalYAML[T any](path string) (T, error) {
	var out T
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, err
	}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}

func (c ClientsConfig) Validate() error {
	ids := map[string]struct{}{}
	for _, target := range c.Targets {
		if strings.TrimSpace(target.ID) == "" {
			return errors.New("clients.yaml: targets[].id is required")
		}
		if _, exists := ids[target.ID]; exists {
			return fmt.Errorf("clients.yaml: duplicate target id %q", target.ID)
		}
		ids[target.ID] = struct{}{}
		if strings.TrimSpace(target.Name) == "" {
			return fmt.Errorf("clients.yaml: targets[%s].name is required", target.ID)
		}
		if strings.TrimSpace(target.Category) == "" {
			return fmt.Errorf("clients.yaml: targets[%s].category is required", target.ID)
		}
		if len(target.Modes) == 0 {
			return fmt.Errorf("clients.yaml: targets[%s].modes must not be empty", target.ID)
		}
		if raw := strings.TrimSpace(target.DocsURL); raw != "" {
			if _, err := url.Parse(raw); err != nil {
				return fmt.Errorf("clients.yaml: targets[%s].docs_url invalid: %w", target.ID, err)
			}
		}
		for _, mode := range target.Modes {
			if strings.TrimSpace(mode.Name) == "" {
				return fmt.Errorf("clients.yaml: targets[%s].modes[].name is required", target.ID)
			}
			if strings.TrimSpace(mode.API) == "" {
				return fmt.Errorf("clients.yaml: targets[%s].modes[%s].api is required", target.ID, mode.Name)
			}
			if raw := strings.TrimSpace(mode.VerifiedOn); raw != "" {
				if _, err := time.Parse("2006-01-02", raw); err != nil {
					return fmt.Errorf("clients.yaml: targets[%s].modes[%s].verified_on must be YYYY-MM-DD: %w", target.ID, mode.Name, err)
				}
			}
			if raw := strings.TrimSpace(mode.Confidence); raw != "" {
				switch raw {
				case "high", "medium", "low":
				default:
					return fmt.Errorf("clients.yaml: targets[%s].modes[%s].confidence must be one of high|medium|low", target.ID, mode.Name)
				}
			}
			if len(mode.RequiredTests) == 0 {
				return fmt.Errorf("clients.yaml: targets[%s].modes[%s].required_tests must not be empty", target.ID, mode.Name)
			}
			for _, testID := range append(append([]string{}, mode.RequiredTests...), append(mode.OptionalTests, mode.UnsupportedOK...)...) {
				if strings.TrimSpace(testID) == "" {
					return fmt.Errorf("clients.yaml: targets[%s].modes[%s] contains an empty test id", target.ID, mode.Name)
				}
			}
		}
	}
	return nil
}

func LoadEnvFiles(paths []string) (map[string]string, error) {
	merged := map[string]string{}
	for _, p := range paths {
		if p == "" {
			continue
		}
		path := p
		if !filepath.IsAbs(path) {
			// keep relative paths relative to cwd
			path = filepath.Clean(path)
		}
		f, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("open env file %s: %w", p, err)
		}
		parsed, err := ParseEnv(f)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("parse env file %s: %w", p, err)
		}
		for k, v := range parsed {
			merged[k] = v
		}
	}
	return merged, nil
}

func ParseEnv(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			return nil, fmt.Errorf("invalid line %d", lineNo)
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if key == "" {
			return nil, fmt.Errorf("invalid key on line %d", lineNo)
		}
		parsed, err := parseEnvValue(val)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		out[key] = parsed
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func parseEnvValue(val string) (string, error) {
	if val == "" {
		return "", nil
	}
	if val[0] == '"' || val[0] == '\'' {
		q := val[0]
		i := 1
		for i < len(val) {
			if val[i] == q {
				if q == '"' && i > 0 && val[i-1] == '\\' {
					i++
					continue
				}
				break
			}
			i++
		}
		if i >= len(val) {
			return "", errors.New("unterminated quote")
		}
		raw := val[:i+1]
		var unq string
		if q == '\'' {
			// Single-quoted values: take content as-is.
			unq = raw[1 : len(raw)-1]
		} else {
			parsed, err := strconv.Unquote(raw)
			if err != nil {
				return "", err
			}
			unq = parsed
		}
		rest := strings.TrimSpace(val[i+1:])
		if rest != "" && !strings.HasPrefix(rest, "#") {
			return "", errors.New("trailing characters after quoted value")
		}
		return unq, nil
	}
	// Unquoted value: strip inline comments preceded by whitespace.
	for i := 0; i < len(val); i++ {
		if val[i] == '#' {
			if i == 0 || val[i-1] == ' ' || val[i-1] == '\t' {
				val = strings.TrimSpace(val[:i])
				break
			}
		}
	}
	return val, nil
}

func normalizeBaseURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimRight(trimmed, "/")
	if strings.HasSuffix(trimmed, "/v1") {
		trimmed = strings.TrimSuffix(trimmed, "/v1")
	}
	return trimmed
}

func ensureLeadingSlash(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
