package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/avelrl/openai-compatible-tester/internal/config"
	"github.com/avelrl/openai-compatible-tester/internal/httpclient"
)

func TestRunnerCapabilityProbeOverridesStaticManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/debug/capabilities" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"capabilities":{"responses.input_items":{"status":"supported"}}}`))
	}))
	defer server.Close()

	cfg := config.Config{
		BaseURL: server.URL,
		Endpoints: config.EndpointsConfig{
			Paths: config.EndpointsPaths{
				DebugCapabilities: "/debug/capabilities",
			},
		},
		Suite: config.SuiteConfig{
			Passes:      1,
			Parallelism: 1,
			Target:      "llama_shim",
		},
		Capabilities: config.CapabilitiesConfig{
			Capabilities: map[string]config.CapabilitySpec{
				"responses.input_items": {Status: config.CapabilityStatusDisabled},
			},
		},
	}
	client := httpclient.New(server.URL, "", nil, time.Second, httpclient.BuildRetryConfigWithRateLimit(1, 0, nil, 1, 0))
	profiles := []config.ModelProfile{{
		Name:           "llama-shim",
		ChatModel:      "shim-chat",
		ResponsesModel: "shim-responses",
	}}

	executed := false
	test := TestCase{
		ID:                   "responses.retrieve.input_items",
		Name:                 "Responses retrieve input_items",
		Target:               "llama_shim",
		RequiredCapabilities: []string{"responses.input_items"},
		Run: func(ctx context.Context, rc RunContext) Result {
			executed = true
			res := baseResult("responses.retrieve.input_items", "Responses retrieve input_items", rc)
			res.Status = StatusPass
			return res
		},
	}

	results, err := NewRunner(cfg, client, []TestCase{test}).Run(context.Background(), profiles, nil)
	if err != nil {
		t.Fatalf("runner returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	if !executed {
		t.Fatal("expected test body to execute after capability probe override")
	}
	if results[0].Status != StatusPass {
		t.Fatalf("unexpected status: %s", results[0].Status)
	}
}

func TestRunnerCapabilityProbeFallsBackToStaticManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := config.Config{
		BaseURL: server.URL,
		Endpoints: config.EndpointsConfig{
			Paths: config.EndpointsPaths{
				DebugCapabilities: "/debug/capabilities",
			},
		},
		Suite: config.SuiteConfig{
			Passes:      1,
			Parallelism: 1,
			Target:      "llama_shim",
		},
		Capabilities: config.CapabilitiesConfig{
			Capabilities: map[string]config.CapabilitySpec{
				"responses.input_items": {Status: config.CapabilityStatusDisabled},
			},
		},
	}
	client := httpclient.New(server.URL, "", nil, time.Second, httpclient.BuildRetryConfigWithRateLimit(1, 0, nil, 1, 0))
	profiles := []config.ModelProfile{{
		Name:           "llama-shim",
		ChatModel:      "shim-chat",
		ResponsesModel: "shim-responses",
	}}

	executed := false
	test := TestCase{
		ID:                   "responses.retrieve.input_items",
		Name:                 "Responses retrieve input_items",
		Target:               "llama_shim",
		RequiredCapabilities: []string{"responses.input_items"},
		Run: func(ctx context.Context, rc RunContext) Result {
			executed = true
			return Result{}
		},
	}

	results, err := NewRunner(cfg, client, []TestCase{test}).Run(context.Background(), profiles, nil)
	if err != nil {
		t.Fatalf("runner returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	if executed {
		t.Fatal("did not expect test body to execute when probe falls back to static disabled manifest")
	}
	if results[0].Status != StatusUnsupported {
		t.Fatalf("unexpected status: %s", results[0].Status)
	}
	if results[0].ErrorType != ErrorTypeCapabilityDisabled {
		t.Fatalf("unexpected error type: %s", results[0].ErrorType)
	}
}

func TestCapabilitiesFromDebugManifestShimShape(t *testing.T) {
	cfg, err := capabilitiesFromDebugManifest([]byte(`{
		"object":"shim.capabilities",
		"ready":false,
		"surfaces":{
			"responses":{"enabled":true,"stateful":true,"retrieve":true,"delete":true,"cancel":true,"input_items":true,"create_stream":true,"retrieve_stream":false,"input_tokens":true,"compact":true,"mode":"prefer_local"},
			"conversations":{"enabled":true,"create":true,"retrieve":true,"items":true},
			"chat_completions":{"enabled":true,"stored":true,"default_store_when_omitted":false},
			"files":{"enabled":true},
			"vector_stores":{"enabled":true},
			"containers":{"enabled":true,"create":true,"files":true}
		},
		"runtime":{
			"responses_mode":"prefer_local",
			"custom_tools_mode":"enabled",
			"codex":{"compatibility_enabled":true,"force_tool_choice_required":false},
			"persistence":{"backend":"sqlite","expected_durable":true},
			"retrieval":{"index_backend":"sqlite"},
			"ops":{"auth_mode":"bearer","rate_limit":{"enabled":true,"requests_per_minute":60,"burst":10},"metrics":{"enabled":true,"path":"/metrics"},"health_public":true,"readyz_public":false}
		},
		"tools":{
			"file_search":{"support":"local","backend":"sqlite","enabled":true,"routing":{"prefer_local":"local","prefer_upstream":"proxy","local_only":"local"}},
			"web_search":{"support":"local","backend":"searxng","enabled":true,"routing":{"prefer_local":"local","prefer_upstream":"proxy","local_only":"local"}},
			"image_generation":{"support":"local","backend":"comfyui","enabled":true,"routing":{"prefer_local":"local","prefer_upstream":"proxy","local_only":"local"}},
			"computer":{"support":"local","backend":"playwright","enabled":false,"routing":{"prefer_local":"local","prefer_upstream":"proxy","local_only":"local"}},
			"code_interpreter":{"support":"local","backend":"container","enabled":true,"routing":{"prefer_local":"local","prefer_upstream":"proxy","local_only":"local"}},
			"mcp_server_url":{"support":"proxy","backend":"mcp","enabled":true,"routing":{"prefer_local":"proxy","prefer_upstream":"proxy","local_only":"reject"}},
			"mcp_connector_id":{"support":"unsupported","backend":"","enabled":false,"routing":{"prefer_local":"reject","prefer_upstream":"reject","local_only":"reject"}},
			"tool_search_hosted":{"support":"proxy","backend":"upstream","enabled":true,"routing":{"prefer_local":"proxy","prefer_upstream":"proxy","local_only":"reject"}},
			"tool_search_client":{"support":"unsupported","backend":"","enabled":false,"routing":{"prefer_local":"reject","prefer_upstream":"reject","local_only":"reject"}}
		},
		"probes":{
			"sqlite":{"enabled":true,"checked":true,"ready":true},
			"llama":{"enabled":true,"checked":true,"ready":true},
			"retrieval_embedder":{"enabled":true,"checked":true,"ready":true},
			"web_search_backend":{"enabled":true,"checked":true,"ready":false},
			"image_generation_backend":{"enabled":true,"checked":true,"ready":true}
		}
	}`))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	assertCapabilityStatus(t, cfg, "responses.store", config.CapabilityStatusSupported)
	assertCapabilityStatus(t, cfg, "responses.input_items", config.CapabilityStatusSupported)
	assertCapabilityStatus(t, cfg, "responses.retrieve_stream", config.CapabilityStatusDisabled)
	assertCapabilityStatus(t, cfg, "conversations.items", config.CapabilityStatusSupported)
	assertCapabilityStatus(t, cfg, "chat.store", config.CapabilityStatusSupported)
	assertCapabilityStatus(t, cfg, "retrieval.vector_store", config.CapabilityStatusSupported)
	assertCapabilityStatus(t, cfg, "tool.file_search.local", config.CapabilityStatusSupported)
	assertCapabilityStatus(t, cfg, "tool.web_search.local", config.CapabilityStatusUnavailable)
	assertCapabilityStatus(t, cfg, "tool.image_generation.local", config.CapabilityStatusSupported)
	assertCapabilityStatus(t, cfg, "tool.computer.local", config.CapabilityStatusDisabled)
	assertCapabilityStatus(t, cfg, "tool.code_interpreter.local", config.CapabilityStatusSupported)
	assertCapabilityStatus(t, cfg, "tool.mcp.server_url", config.CapabilityStatusSupported)
	assertCapabilityStatus(t, cfg, "tool.tool_search.server", config.CapabilityStatusSupported)
	assertCapabilityStatus(t, cfg, "persistence.restart_safe", config.CapabilityStatusSupported)
}

func assertCapabilityStatus(t *testing.T, cfg config.CapabilitiesConfig, name, want string) {
	t.Helper()
	spec, ok := cfg.Lookup(name)
	if !ok {
		t.Fatalf("missing capability %s", name)
	}
	if spec.Status != want {
		t.Fatalf("capability %s: got %s want %s", name, spec.Status, want)
	}
}
