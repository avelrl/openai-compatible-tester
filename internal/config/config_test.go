package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEnv(t *testing.T) {
	input := `# comment
export FOO=bar
BAZ="quoted value"
SINGLE='one two'
EMPTY=
INLINE=ok # comment
`
	r := strings.NewReader(input)
	vals, err := ParseEnv(r)
	if err != nil {
		t.Fatalf("ParseEnv error: %v", err)
	}
	if vals["FOO"] != "bar" {
		t.Fatalf("FOO=%q", vals["FOO"])
	}
	if vals["BAZ"] != "quoted value" {
		t.Fatalf("BAZ=%q", vals["BAZ"])
	}
	if vals["SINGLE"] != "one two" {
		t.Fatalf("SINGLE=%q", vals["SINGLE"])
	}
	if _, ok := vals["EMPTY"]; !ok {
		t.Fatalf("EMPTY missing")
	}
	if vals["INLINE"] != "ok" {
		t.Fatalf("INLINE=%q", vals["INLINE"])
	}
}

func TestLoadConfigPrecedence(t *testing.T) {
	dir := t.TempDir()
	suite := filepath.Join(dir, "suite.yaml")
	models := filepath.Join(dir, "models.yaml")
	endpoints := filepath.Join(dir, "endpoints.yaml")
	clients := filepath.Join(dir, "clients.yaml")
	if err := os.WriteFile(suite, []byte("passes: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(models, []byte("profiles:\n  - name: default\n    chat_model: chat\n    responses_model: resp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(endpoints, []byte("base_url: https://example.com\napi_key_env: OPENAI_API_KEY\npaths:\n  models: /v1/models\n  chat: /v1/chat/completions\n  responses: /v1/responses\n  conversations: /v1/conversations\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(clients, []byte("targets:\n  - id: codex-cli\n    name: Codex CLI\n    category: coding_agent\n    modes:\n      - name: responses\n        api: responses\n        required_tests:\n          - responses.basic\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("OPENAI_API_KEY=fromenv\nBASE_URL=https://env.example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(LoadOptions{
		SuitePath:     suite,
		ModelsPath:    models,
		EndpointsPath: endpoints,
		ClientsPath:   clients,
		EnvFile:       filepath.Join(dir, ".env"),
		BaseURL:       "https://flag.example.com",
		APIKey:        "flagkey",
	})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.BaseURL != "https://flag.example.com" {
		t.Fatalf("BaseURL=%q", cfg.BaseURL)
	}
	if cfg.APIKey != "flagkey" {
		t.Fatalf("APIKey=%q", cfg.APIKey)
	}
	if len(cfg.Clients.Targets) != 1 || cfg.Clients.Targets[0].ID != "codex-cli" {
		t.Fatalf("clients not loaded: %+v", cfg.Clients.Targets)
	}
}

func TestLoadRejectsOverlappingBaseURLAndPaths(t *testing.T) {
	dir := t.TempDir()
	suite := filepath.Join(dir, "suite.yaml")
	models := filepath.Join(dir, "models.yaml")
	endpoints := filepath.Join(dir, "endpoints.yaml")
	if err := os.WriteFile(suite, []byte("passes: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(models, []byte("profiles:\n  - name: default\n    chat_model: chat\n    responses_model: resp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(endpoints, []byte("base_url: https://example.com/proxy/openai\napi_key_env: OPENAI_API_KEY\npaths:\n  models: /proxy/openai/v1/models\n  chat: /proxy/openai/v1/chat/completions\n  responses: /proxy/openai/v1/responses\n  conversations: /proxy/openai/v1/conversations\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("OPENAI_API_KEY=fromenv\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(LoadOptions{
		SuitePath:     suite,
		ModelsPath:    models,
		EndpointsPath: endpoints,
		EnvFile:       filepath.Join(dir, ".env"),
	})
	if err == nil {
		t.Fatal("expected overlap validation error")
	}
	if !strings.Contains(err.Error(), "overlaps with base_url path") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadAllowsMissingAPIKeyForUnauthenticatedTargets(t *testing.T) {
	dir := t.TempDir()
	suite := filepath.Join(dir, "suite.yaml")
	models := filepath.Join(dir, "models.yaml")
	endpoints := filepath.Join(dir, "endpoints.yaml")
	if err := os.WriteFile(suite, []byte("passes: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(models, []byte("profiles:\n  - name: default\n    chat_model: chat\n    responses_model: resp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(endpoints, []byte("base_url: http://127.0.0.1:1234\napi_key_env: OPENAI_API_KEY\npaths:\n  models: /v1/models\n  chat: /v1/chat/completions\n  responses: /v1/responses\n  conversations: /v1/conversations\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(LoadOptions{
		SuitePath:     suite,
		ModelsPath:    models,
		EndpointsPath: endpoints,
	})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.APIKey != "" {
		t.Fatalf("APIKey=%q, want empty", cfg.APIKey)
	}
}

func TestLoadRejectsInvalidClientVerificationMetadata(t *testing.T) {
	dir := t.TempDir()
	suite := filepath.Join(dir, "suite.yaml")
	models := filepath.Join(dir, "models.yaml")
	endpoints := filepath.Join(dir, "endpoints.yaml")
	clients := filepath.Join(dir, "clients.yaml")
	if err := os.WriteFile(suite, []byte("passes: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(models, []byte("profiles:\n  - name: default\n    chat_model: chat\n    responses_model: resp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(endpoints, []byte("base_url: https://example.com\napi_key_env: OPENAI_API_KEY\npaths:\n  models: /v1/models\n  chat: /v1/chat/completions\n  responses: /v1/responses\n  conversations: /v1/conversations\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(clients, []byte("targets:\n  - id: codex-cli\n    name: Codex CLI\n    category: coding_agent\n    modes:\n      - name: responses\n        api: responses\n        verified_on: not-a-date\n        confidence: certain\n        required_tests:\n          - responses.basic\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("OPENAI_API_KEY=fromenv\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(LoadOptions{
		SuitePath:     suite,
		ModelsPath:    models,
		EndpointsPath: endpoints,
		ClientsPath:   clients,
		EnvFile:       filepath.Join(dir, ".env"),
	})
	if err == nil {
		t.Fatal("expected client metadata validation error")
	}
	if !strings.Contains(err.Error(), "verified_on") && !strings.Contains(err.Error(), "confidence") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadAcceptsProfileTestOverrides(t *testing.T) {
	dir := t.TempDir()
	suite := filepath.Join(dir, "suite.yaml")
	models := filepath.Join(dir, "models.yaml")
	endpoints := filepath.Join(dir, "endpoints.yaml")
	if err := os.WriteFile(suite, []byte("passes: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(models, []byte("profiles:\n  - name: kimi-tuned\n    chat_model: chat\n    responses_model: resp\n    tests:\n      chat.tool_call.required:\n        max_tokens: 128\n        reasoning_effort: omit\n        instruction_role: system\n        instruction_text: Use the tool only.\n        user_text: Call add.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(endpoints, []byte("base_url: https://example.com\napi_key_env: OPENAI_API_KEY\npaths:\n  models: /v1/models\n  chat: /v1/chat/completions\n  responses: /v1/responses\n  conversations: /v1/conversations\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("OPENAI_API_KEY=fromenv\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(LoadOptions{
		SuitePath:     suite,
		ModelsPath:    models,
		EndpointsPath: endpoints,
		EnvFile:       filepath.Join(dir, ".env"),
	})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	profile := cfg.Models.Profiles[0]
	ov, ok := profile.Tests["chat.tool_call.required"]
	if !ok {
		t.Fatalf("expected profile test override")
	}
	if ov.MaxTokens == nil || *ov.MaxTokens != 128 {
		t.Fatalf("max_tokens=%v, want 128", ov.MaxTokens)
	}
	if ov.ReasoningEffort != "omit" {
		t.Fatalf("reasoning_effort=%q", ov.ReasoningEffort)
	}
	if ov.InstructionRole != "system" {
		t.Fatalf("instruction_role=%q", ov.InstructionRole)
	}
	if ov.InstructionText != "Use the tool only." {
		t.Fatalf("instruction_text=%q", ov.InstructionText)
	}
	if ov.UserText != "Call add." {
		t.Fatalf("user_text=%q", ov.UserText)
	}
	if profile.RateLimitPerMinute != 0 {
		t.Fatalf("rate_limit_per_minute=%d, want 0 by default", profile.RateLimitPerMinute)
	}
}

func TestLoadAcceptsProfileRateLimit(t *testing.T) {
	dir := t.TempDir()
	suite := filepath.Join(dir, "suite.yaml")
	models := filepath.Join(dir, "models.yaml")
	endpoints := filepath.Join(dir, "endpoints.yaml")
	if err := os.WriteFile(suite, []byte("passes: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(models, []byte("profiles:\n  - name: throttled\n    chat_model: chat\n    responses_model: resp\n    rate_limit_per_minute: 40\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(endpoints, []byte("base_url: https://example.com\napi_key_env: OPENAI_API_KEY\npaths:\n  models: /v1/models\n  chat: /v1/chat/completions\n  responses: /v1/responses\n  conversations: /v1/conversations\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("OPENAI_API_KEY=fromenv\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(LoadOptions{
		SuitePath:     suite,
		ModelsPath:    models,
		EndpointsPath: endpoints,
		EnvFile:       filepath.Join(dir, ".env"),
	})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if got := cfg.Models.Profiles[0].RateLimitPerMinute; got != 40 {
		t.Fatalf("rate_limit_per_minute=%d, want 40", got)
	}
}

func TestLoadAppliesRateLimitRetryDefaultsAndOverrides(t *testing.T) {
	dir := t.TempDir()
	suite := filepath.Join(dir, "suite.yaml")
	models := filepath.Join(dir, "models.yaml")
	endpoints := filepath.Join(dir, "endpoints.yaml")
	if err := os.WriteFile(suite, []byte("passes: 1\nretry:\n  max_attempts: 4\n  backoff_ms: 900\n  rate_limit_max_attempts: 2\n  rate_limit_fallback_ms: 5000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(models, []byte("profiles:\n  - name: throttled\n    chat_model: chat\n    responses_model: resp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(endpoints, []byte("base_url: https://example.com\napi_key_env: OPENAI_API_KEY\npaths:\n  models: /v1/models\n  chat: /v1/chat/completions\n  responses: /v1/responses\n  conversations: /v1/conversations\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("OPENAI_API_KEY=fromenv\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(LoadOptions{
		SuitePath:     suite,
		ModelsPath:    models,
		EndpointsPath: endpoints,
		EnvFile:       filepath.Join(dir, ".env"),
	})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if got := cfg.Suite.Retry.MaxAttempts; got != 4 {
		t.Fatalf("max_attempts=%d, want 4", got)
	}
	if got := cfg.Suite.Retry.BackoffMS; got != 900 {
		t.Fatalf("backoff_ms=%d, want 900", got)
	}
	if got := cfg.Suite.Retry.RateLimitMaxAttempts; got != 2 {
		t.Fatalf("rate_limit_max_attempts=%d, want 2", got)
	}
	if got := cfg.Suite.Retry.RateLimitFallbackMS; got != 5000 {
		t.Fatalf("rate_limit_fallback_ms=%d, want 5000", got)
	}
}

func TestLoadInheritsRateLimitRetryDefaultsFromGenericRetry(t *testing.T) {
	dir := t.TempDir()
	suite := filepath.Join(dir, "suite.yaml")
	models := filepath.Join(dir, "models.yaml")
	endpoints := filepath.Join(dir, "endpoints.yaml")
	if err := os.WriteFile(suite, []byte("passes: 1\nretry:\n  max_attempts: 2\n  backoff_ms: 700\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(models, []byte("profiles:\n  - name: throttled\n    chat_model: chat\n    responses_model: resp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(endpoints, []byte("base_url: https://example.com\napi_key_env: OPENAI_API_KEY\npaths:\n  models: /v1/models\n  chat: /v1/chat/completions\n  responses: /v1/responses\n  conversations: /v1/conversations\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("OPENAI_API_KEY=fromenv\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(LoadOptions{
		SuitePath:     suite,
		ModelsPath:    models,
		EndpointsPath: endpoints,
		EnvFile:       filepath.Join(dir, ".env"),
	})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if got := cfg.Suite.Retry.RateLimitMaxAttempts; got != 2 {
		t.Fatalf("rate_limit_max_attempts=%d, want inherited 2", got)
	}
	if got := cfg.Suite.Retry.RateLimitFallbackMS; got != 700 {
		t.Fatalf("rate_limit_fallback_ms=%d, want inherited 700", got)
	}
}

func TestLoadRejectsInvalidInstructionRole(t *testing.T) {
	dir := t.TempDir()
	suite := filepath.Join(dir, "suite.yaml")
	models := filepath.Join(dir, "models.yaml")
	endpoints := filepath.Join(dir, "endpoints.yaml")
	if err := os.WriteFile(suite, []byte("passes: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(models, []byte("profiles:\n  - name: bad\n    chat_model: chat\n    responses_model: resp\n    tests:\n      chat.basic:\n        instruction_role: assistant\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(endpoints, []byte("base_url: https://example.com\napi_key_env: OPENAI_API_KEY\npaths:\n  models: /v1/models\n  chat: /v1/chat/completions\n  responses: /v1/responses\n  conversations: /v1/conversations\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("OPENAI_API_KEY=fromenv\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(LoadOptions{
		SuitePath:     suite,
		ModelsPath:    models,
		EndpointsPath: endpoints,
		EnvFile:       filepath.Join(dir, ".env"),
	})
	if err == nil {
		t.Fatal("expected invalid instruction_role error")
	}
	if !strings.Contains(err.Error(), "instruction_role") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRejectsNegativeProfileRateLimit(t *testing.T) {
	dir := t.TempDir()
	suite := filepath.Join(dir, "suite.yaml")
	models := filepath.Join(dir, "models.yaml")
	endpoints := filepath.Join(dir, "endpoints.yaml")
	if err := os.WriteFile(suite, []byte("passes: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(models, []byte("profiles:\n  - name: bad\n    chat_model: chat\n    responses_model: resp\n    rate_limit_per_minute: -1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(endpoints, []byte("base_url: https://example.com\napi_key_env: OPENAI_API_KEY\npaths:\n  models: /v1/models\n  chat: /v1/chat/completions\n  responses: /v1/responses\n  conversations: /v1/conversations\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("OPENAI_API_KEY=fromenv\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(LoadOptions{
		SuitePath:     suite,
		ModelsPath:    models,
		EndpointsPath: endpoints,
		EnvFile:       filepath.Join(dir, ".env"),
	})
	if err == nil {
		t.Fatal("expected invalid rate_limit_per_minute error")
	}
	if !strings.Contains(err.Error(), "rate_limit_per_minute") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRejectsInvalidRateLimitRetryConfig(t *testing.T) {
	dir := t.TempDir()
	suite := filepath.Join(dir, "suite.yaml")
	models := filepath.Join(dir, "models.yaml")
	endpoints := filepath.Join(dir, "endpoints.yaml")
	if err := os.WriteFile(suite, []byte("passes: 1\nretry:\n  rate_limit_max_attempts: -1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(models, []byte("profiles:\n  - name: bad\n    chat_model: chat\n    responses_model: resp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(endpoints, []byte("base_url: https://example.com\napi_key_env: OPENAI_API_KEY\npaths:\n  models: /v1/models\n  chat: /v1/chat/completions\n  responses: /v1/responses\n  conversations: /v1/conversations\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("OPENAI_API_KEY=fromenv\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(LoadOptions{
		SuitePath:     suite,
		ModelsPath:    models,
		EndpointsPath: endpoints,
		EnvFile:       filepath.Join(dir, ".env"),
	})
	if err == nil {
		t.Fatal("expected invalid rate limit retry config error")
	}
	if !strings.Contains(err.Error(), "rate_limit_max_attempts") {
		t.Fatalf("unexpected error: %v", err)
	}
}
