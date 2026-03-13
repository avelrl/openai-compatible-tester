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
