package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/avelrl/openai-compatible-tester/internal/config"
	"github.com/avelrl/openai-compatible-tester/internal/httpclient"
)

func TestIsUnknownParam(t *testing.T) {
	body := []byte(`{"error":{"message":"Unknown parameter: response_format"}}`)
	if !isUnknownParam(body) {
		t.Fatalf("expected unknown param")
	}
}

func TestErrorMessageStringError(t *testing.T) {
	body := []byte(`{"error":"Unexpected endpoint or method. (POST /v1/conversations)"}`)
	if got := errorMessage(body); got == "" {
		t.Fatalf("expected string error to be parsed")
	}
}

func TestIsUnsupportedFeature(t *testing.T) {
	tests := []string{
		`{"error":"Invalid tool_choice type: 'object'. Supported string values: none, auto, required"}`,
		`{"error":{"message":"Expected 'none' | 'auto' | 'required', received object"}}`,
		`{"error":"'response_format.type' must be 'json_schema' or 'text'"}`,
		`{"error":"Input should be 'text' or 'json_object'"}`,
		`{"error":{"code":"invalid_prompt","message":"Invalid Responses API request"},"metadata":{"raw":"[{\"path\":[\"store\"],\"message\":\"Invalid input: expected false\"}]"}}`,
	}
	for _, body := range tests {
		if !isUnsupportedFeature([]byte(body)) {
			t.Fatalf("expected unsupported feature for %s", body)
		}
	}
}

func TestIsUnexpectedEndpointOrMethod(t *testing.T) {
	body := []byte(`{"error":"Unexpected endpoint or method. (GET /v1/responses/resp_123)"}`)
	if !isUnexpectedEndpointOrMethod(body) {
		t.Fatalf("expected unsupported endpoint classification")
	}
}

func TestIsHTMLDocument(t *testing.T) {
	body := []byte(`<!DOCTYPE html><html><head></head><body>nope</body></html>`)
	if !isHTMLDocument(body) {
		t.Fatalf("expected HTML document classification")
	}
}

func TestTrimLeadingBlankLines(t *testing.T) {
	in := "\n  \n\t\n{\"ok\":true}\n"
	if got := trimLeadingBlankLines(in); got != "{\"ok\":true}\n" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractPreviousResponseID(t *testing.T) {
	body := []byte(`{"previous_response_id":"resp_123"}`)
	if got := extractPreviousResponseID(body); got != "resp_123" {
		t.Fatalf("got %q", got)
	}
}

func TestErrorMessageRateLimitHint(t *testing.T) {
	body := []byte(`{"error":{"message":"Rate limit exceeded: free-models-per-min.","code":429,"metadata":{"headers":{"X-RateLimit-Reset":"4102444800000"}}}}`)
	got := errorMessage(body)
	if !strings.Contains(got, "Rate limit exceeded") {
		t.Fatalf("missing base error message: %q", got)
	}
	if !strings.Contains(got, "retry in ~") {
		t.Fatalf("missing retry hint: %q", got)
	}
}

func TestFailHTTPStatusResultAppendsRelevantHeaders(t *testing.T) {
	result := Result{
		TraceSteps:   []TraceStep{{Name: "main", Response: "SSE:\n\nTEXT:\n"}},
		snippetLimit: 4096,
	}
	resp := &httpclient.StreamResult{
		Response: httpclient.Response{
			StatusCode: http.StatusTooManyRequests,
			Headers: http.Header{
				"Retry-After":           []string{"12"},
				"X-RateLimit-Remaining": []string{"0"},
			},
		},
	}

	got := failHTTPStatusResult(result, resp)
	if !strings.Contains(got.ErrorMessage, "retry in ~12s") {
		t.Fatalf("missing retry hint in error message: %q", got.ErrorMessage)
	}
	if len(got.TraceSteps) != 1 {
		t.Fatalf("expected one trace step, got %d", len(got.TraceSteps))
	}
	if !strings.Contains(got.TraceSteps[0].Response, "HEADERS:") {
		t.Fatalf("missing headers in trace: %q", got.TraceSteps[0].Response)
	}
	if !strings.Contains(got.TraceSteps[0].Response, "Retry-After: 12") {
		t.Fatalf("missing Retry-After header in trace: %q", got.TraceSteps[0].Response)
	}
}

func TestWrappedUpstreamStatus(t *testing.T) {
	body := []byte(`{"error":{"message":"litellm.APIConnectionError: validation error input_value={'error': {'message': 'Find response', 'code': 404}}"}}`)
	if got := wrappedUpstreamStatus(body, 500); got != 404 {
		t.Fatalf("got %d", got)
	}
	if got := wrappedUpstreamStatus(body, 400); got != 0 {
		t.Fatalf("did not expect wrapped status for non-5xx, got %d", got)
	}
}

func TestParseChatStreamEvent(t *testing.T) {
	data := `{"choices":[{"delta":{"content":"Hi"}}]}`
	delta, done := parseChatStreamEvent(data)
	if delta != "Hi" || done {
		t.Fatalf("delta=%q done=%v", delta, done)
	}

	data = `{"choices":[{"delta":{"content":"\nE\nL\nL\nO"},"finish_reason":"stop"}]}`
	delta, done = parseChatStreamEvent(data)
	if delta != "\nE\nL\nL\nO" || !done {
		t.Fatalf("final delta=%q done=%v", delta, done)
	}

	_, done = parseChatStreamEvent("[DONE]")
	if !done {
		t.Fatalf("expected done")
	}
}

func TestParseResponsesStreamEvent(t *testing.T) {
	data := `{"type":"response.output_text.delta","delta":"OK"}`
	delta, done := parseResponsesStreamEvent(data)
	if delta != "OK" || done {
		t.Fatalf("delta=%q done=%v", delta, done)
	}

	data = `{"type":"response.content_part.added","part":{"type":"output_text","text":"OK"}}`
	delta, done = parseResponsesStreamEvent(data)
	if delta != "OK" || done {
		t.Fatalf("delta=%q done=%v", delta, done)
	}

	data = `{"type":"response.reasoning_text.delta","delta":"HELLO"}`
	delta, done = parseResponsesStreamEvent(data)
	if delta != "" || done {
		t.Fatalf("reasoning delta should be ignored, delta=%q done=%v", delta, done)
	}

	data = `{"type":"response.output_text.done","text":"OK"}`
	delta, done = parseResponsesStreamEvent(data)
	if delta != "OK" || !done {
		t.Fatalf("delta=%q done=%v", delta, done)
	}

	_, done = parseResponsesStreamEvent("[DONE]")
	if !done {
		t.Fatalf("expected done")
	}
}

func TestApplyChatReasoningOverride(t *testing.T) {
	payload := map[string]interface{}{
		"model":     "m",
		"reasoning": map[string]interface{}{"effort": "minimal"},
	}

	applyChatReasoningOverride(payload, "omit")
	if _, ok := payload["reasoning"]; ok {
		t.Fatalf("expected reasoning to be removed")
	}

	applyChatReasoningOverride(payload, "minimal")
	reasoning, ok := payload["reasoning"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected reasoning map")
	}
	if got := reasoning["effort"]; got != "minimal" {
		t.Fatalf("got %v", got)
	}
}

func TestApplyResponsesReasoningOverride(t *testing.T) {
	payload := map[string]interface{}{
		"model":     "m",
		"reasoning": map[string]interface{}{"effort": "minimal"},
	}

	applyResponsesReasoningOverride(payload, "omit")
	if _, ok := payload["reasoning"]; ok {
		t.Fatalf("expected reasoning to be removed")
	}

	applyResponsesReasoningOverride(payload, "minimal")
	reasoning, ok := payload["reasoning"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected reasoning map")
	}
	if got := reasoning["effort"]; got != "minimal" {
		t.Fatalf("got %v", got)
	}
}

func TestEffectiveChatMaxTokens(t *testing.T) {
	v128 := 128
	v96 := 96

	if got := effectiveChatMaxTokens(config.TestOverride{}); got != 64 {
		t.Fatalf("default max tokens = %d, want 64", got)
	}
	if got := effectiveChatMaxTokens(config.TestOverride{MaxTokens: &v128}); got != 128 {
		t.Fatalf("max_tokens override = %d, want 128", got)
	}
	if got := effectiveChatMaxTokens(config.TestOverride{MaxOutputTokens: &v96}); got != 96 {
		t.Fatalf("max_output_tokens fallback = %d, want 96", got)
	}
}

func TestEffectiveResponsesMaxOutputTokens(t *testing.T) {
	v128 := 128
	v0 := 0

	if got := effectiveResponsesMaxOutputTokens(config.TestOverride{}); got != nil {
		t.Fatalf("expected nil by default, got %v", *got)
	}
	if got := effectiveResponsesMaxOutputTokens(config.TestOverride{MaxOutputTokens: &v128}); got == nil || *got != 128 {
		if got == nil {
			t.Fatalf("expected 128, got nil")
		}
		t.Fatalf("expected 128, got %d", *got)
	}
	if got := effectiveResponsesMaxOutputTokens(config.TestOverride{MaxOutputTokens: &v0}); got != nil {
		t.Fatalf("expected nil for zero override, got %v", *got)
	}
}

func TestChatMaxTokensOverride(t *testing.T) {
	v48 := 48
	v72 := 72

	if got := chatMaxTokensOverride(config.TestOverride{}); got != nil {
		t.Fatalf("expected nil by default, got %v", *got)
	}
	if got := chatMaxTokensOverride(config.TestOverride{MaxTokens: &v48}); got == nil || *got != 48 {
		if got == nil {
			t.Fatalf("expected 48, got nil")
		}
		t.Fatalf("expected 48, got %d", *got)
	}
	if got := chatMaxTokensOverride(config.TestOverride{MaxOutputTokens: &v72}); got == nil || *got != 72 {
		if got == nil {
			t.Fatalf("expected 72, got nil")
		}
		t.Fatalf("expected 72, got %d", *got)
	}
}

func TestEffectiveChatReasoningEffort(t *testing.T) {
	profile := config.ModelProfile{ReasoningEffort: "minimal"}
	suite := config.SuiteConfig{ChatReasoning: config.Toggle{Enabled: true}}

	if got := effectiveChatReasoningEffort(profile, suite, config.TestOverride{}); got != "minimal" {
		t.Fatalf("expected profile reasoning, got %q", got)
	}
	if got := effectiveChatReasoningEffort(profile, suite, config.TestOverride{ReasoningEffort: "omit"}); got != "omit" {
		t.Fatalf("expected explicit omit, got %q", got)
	}
	if got := effectiveChatReasoningEffort(profile, config.SuiteConfig{}, config.TestOverride{}); got != "omit" {
		t.Fatalf("expected omit when chat reasoning disabled, got %q", got)
	}
}

func TestEffectiveResponsesReasoningEffort(t *testing.T) {
	profile := config.ModelProfile{ReasoningEffort: "minimal"}

	if got := effectiveResponsesReasoningEffort(profile, config.TestOverride{}); got != "minimal" {
		t.Fatalf("expected profile reasoning, got %q", got)
	}
	if got := effectiveResponsesReasoningEffort(profile, config.TestOverride{ReasoningEffort: "omit"}); got != "omit" {
		t.Fatalf("expected explicit omit, got %q", got)
	}
	if got := effectiveResponsesReasoningEffort(config.ModelProfile{}, config.TestOverride{}); got != "omit" {
		t.Fatalf("expected omit without profile reasoning, got %q", got)
	}
}

func TestApplyStreamTimeoutHeader(t *testing.T) {
	cfg := config.Config{
		Suite: config.SuiteConfig{
			Tests: map[string]config.TestOverride{},
		},
	}
	headers := map[string]string{"Accept": "text/event-stream"}
	ov := config.TestOverride{StreamTimeoutSeconds: 15}

	applyStreamTimeoutHeader(headers, cfg, config.ModelProfile{}, "responses.stream", ov)
	if got := headers["x-litellm-stream-timeout"]; got != "15" {
		t.Fatalf("got %q", got)
	}

	cfg.Suite.Tests["responses.stream"] = config.TestOverride{
		LiteLLMHeaders: map[string]string{"x-litellm-stream-timeout": "99"},
	}
	headers = map[string]string{"Accept": "text/event-stream", "x-litellm-stream-timeout": "99"}
	applyStreamTimeoutHeader(headers, cfg, config.ModelProfile{}, "responses.stream", ov)
	if got := headers["x-litellm-stream-timeout"]; got != "99" {
		t.Fatalf("header override should win, got %q", got)
	}
}

func TestTestOverrideForProfileMergesSuiteDefaults(t *testing.T) {
	v96 := 96
	cfg := config.Config{
		Suite: config.SuiteConfig{
			Tests: map[string]config.TestOverride{
				"chat.tool_call": {
					TimeoutSeconds:  60,
					MaxTokens:       &v96,
					InstructionRole: "developer",
					LiteLLMHeaders:  map[string]string{"x-litellm-timeout": "60"},
				},
			},
		},
	}
	profile := config.ModelProfile{
		Name: "kimi-tuned",
		Tests: map[string]config.TestOverride{
			"chat.tool_call.required": {
				TimeoutSeconds:  45,
				ReasoningEffort: "omit",
				InstructionRole: "system",
			},
		},
	}

	got, ok := testOverrideForProfile(cfg, profile, "chat.tool_call.required")
	if !ok {
		t.Fatalf("expected merged override")
	}
	if got.TimeoutSeconds != 45 {
		t.Fatalf("timeout=%d, want 45", got.TimeoutSeconds)
	}
	if got.MaxTokens == nil || *got.MaxTokens != 96 {
		if got.MaxTokens == nil {
			t.Fatalf("expected inherited max_tokens")
		}
		t.Fatalf("max_tokens=%d, want 96", *got.MaxTokens)
	}
	if got.ReasoningEffort != "omit" {
		t.Fatalf("reasoning=%q, want omit", got.ReasoningEffort)
	}
	if got.InstructionRole != "system" {
		t.Fatalf("instruction_role=%q, want system", got.InstructionRole)
	}
	if got.LiteLLMHeaders["x-litellm-timeout"] != "60" {
		t.Fatalf("expected inherited header, got %q", got.LiteLLMHeaders["x-litellm-timeout"])
	}
}

func TestEffectiveInstructionRole(t *testing.T) {
	cfg := config.Config{
		Suite: config.SuiteConfig{
			Tests: map[string]config.TestOverride{
				"chat.basic": {InstructionRole: "system"},
			},
		},
	}
	profile := config.ModelProfile{
		Tests: map[string]config.TestOverride{
			"chat.stream": {InstructionRole: "user"},
		},
	}

	if got := effectiveInstructionRole(cfg, profile, "chat.basic", "developer"); got != "system" {
		t.Fatalf("chat.basic role=%q", got)
	}
	if got := effectiveInstructionRole(cfg, profile, "chat.stream", "developer"); got != "user" {
		t.Fatalf("chat.stream role=%q", got)
	}
	if got := effectiveInstructionRole(cfg, profile, "chat.structured.json_schema", "system"); got != "system" {
		t.Fatalf("default role=%q", got)
	}
}

func TestResponsesInputWithOptionalInstructionPreservesFallback(t *testing.T) {
	got := responsesInputWithOptionalInstruction(config.Config{}, config.ModelProfile{}, "responses.basic", "", "Reply with exactly OK", "ping", "ping: answer with OK")
	raw, ok := got.(string)
	if !ok {
		t.Fatalf("expected string fallback, got %T", got)
	}
	if raw != "ping: answer with OK" {
		t.Fatalf("fallback=%q", raw)
	}
}

func TestResponsesInputWithOptionalInstructionUsesConfiguredRole(t *testing.T) {
	cfg := config.Config{
		Suite: config.SuiteConfig{
			Tests: map[string]config.TestOverride{
				"responses.basic": {InstructionRole: "system"},
			},
		},
	}
	got := responsesInputWithOptionalInstruction(cfg, config.ModelProfile{}, "responses.basic", "", "Reply with exactly OK", "ping", "ping: answer with OK")
	items, ok := got.([]map[string]string)
	if !ok {
		t.Fatalf("expected message input, got %T", got)
	}
	if len(items) != 2 {
		t.Fatalf("len(items)=%d", len(items))
	}
	if items[0]["role"] != "system" || items[0]["content"] != "Reply with exactly OK" {
		t.Fatalf("instruction item=%v", items[0])
	}
	if items[1]["role"] != "user" || items[1]["content"] != "ping" {
		t.Fatalf("user item=%v", items[1])
	}
}

func TestExtractResponseTextChatStyle(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"OK"}}]}`)
	if got := extractResponseText(body); got != "OK" {
		t.Fatalf("got %q", got)
	}
	body = []byte(`{"choices":[{"message":{"content":[{"type":"output_text","text":"OK"}]}}]}`)
	if got := extractResponseText(body); got != "OK" {
		t.Fatalf("got %q", got)
	}
}

func TestMatchesExpectedTextStrict(t *testing.T) {
	if !matchesExpectedText("42", "42", false) {
		t.Fatalf("expected strict exact match")
	}
	if matchesExpectedText("42\n42", "42", false) {
		t.Fatalf("did not expect repeated scalar to match in strict mode")
	}
}

func TestMatchesExpectedTextAcceptsRepeatedScalarWhenEnabled(t *testing.T) {
	if !matchesExpectedText("42\n42", "42", true) {
		t.Fatalf("expected repeated scalar response to match")
	}
	if !matchesExpectedText("42 42", "42", true) {
		t.Fatalf("expected repeated scalar tokens to match")
	}
	if matchesExpectedText("42 because tool said so", "42", true) {
		t.Fatalf("unexpected loose match")
	}
}

func TestValidateAddToolCallArgs(t *testing.T) {
	if err := validateAddToolCallArgs(`{"a":40,"b":2}`, 40, 2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := validateAddToolCallArgs(`{}`, 40, 2); err == nil {
		t.Fatalf("expected empty args to fail")
	}
	if err := validateAddToolCallArgs(`{"a":1,"b":2}`, 40, 2); err == nil {
		t.Fatalf("expected wrong args to fail")
	}
}

func TestIsValidStructuredStrictSchema(t *testing.T) {
	if !isValidStructured(`{"status":"ok","value":42}`, true) {
		t.Fatalf("expected exact schema payload to pass")
	}
	if isValidStructured(`{"status":"ok","value":42,"extra":1}`, true) {
		t.Fatalf("did not expect extra properties to pass")
	}
	if isValidStructured(`{"status":"wrong","value":42}`, true) {
		t.Fatalf("did not expect wrong status to pass")
	}
	if isValidStructured(`{"status":"ok","value":41}`, true) {
		t.Fatalf("did not expect wrong value to pass")
	}
}

func TestBuildResponsesToolFollowupInputNormalizesItems(t *testing.T) {
	body := []byte(`{
		"output": [
			{
				"id": "rs_tmp_1",
				"type": "reasoning",
				"content": [{"type": "reasoning_text", "text": "thinking"}]
			},
			{
				"id": "fc_tmp_1",
				"type": "function_call",
				"call_id": "call_123",
				"name": "add",
				"arguments": "{\"a\":40,\"b\":2}"
			}
		]
	}`)

	input := buildResponsesToolFollowupInput(body, "call_123", `{"a":40,"b":2}`, "add", "Call add", "Reply with just the number.", `{"result":42}`)
	if len(input) != 4 {
		t.Fatalf("expected 4 follow-up items, got %d", len(input))
	}
	call, ok := input[1].(map[string]interface{})
	if !ok {
		t.Fatalf("expected function_call item")
	}
	if got := call["type"]; got != "function_call" {
		t.Fatalf("unexpected type %v", got)
	}
	if got := call["call_id"]; got != "call_123" {
		t.Fatalf("unexpected call_id %v", got)
	}
	if _, hasReasoning := call["content"]; hasReasoning {
		t.Fatalf("function_call item should not carry reasoning content")
	}
	out, ok := input[2].(map[string]interface{})
	if !ok {
		t.Fatalf("expected function_call_output item")
	}
	if got := out["type"]; got != "function_call_output" {
		t.Fatalf("unexpected output type %v", got)
	}
	if got := out["call_id"]; got != "call_123" {
		t.Fatalf("unexpected output call_id %v", got)
	}
	if _, ok := out["id"].(string); !ok {
		t.Fatalf("expected output item id to be populated")
	}
}

func TestEffectiveTraceStepsFallsBackToMainSnippet(t *testing.T) {
	res := Result{
		RequestSnippet:  "req",
		ResponseSnippet: "resp",
	}
	steps := EffectiveTraceSteps(res)
	if len(steps) != 1 {
		t.Fatalf("expected 1 fallback step, got %d", len(steps))
	}
	if steps[0].Name != "main" || steps[0].Request != "req" || steps[0].Response != "resp" {
		t.Fatalf("unexpected fallback step: %+v", steps[0])
	}
}

func TestRunChatStreamAppliesReasoningOverride(t *testing.T) {
	var gotBody map[string]interface{}
	cfg := config.Config{
		BaseURL: "https://example.test",
		Endpoints: config.EndpointsConfig{
			Paths: config.EndpointsPaths{
				Chat: "/v1/chat/completions",
			},
		},
		Suite: config.SuiteConfig{
			ChatReasoning: config.Toggle{Enabled: true},
			Report:        config.ReportConfig{SnippetLimitBytes: 4096},
		},
	}
	profile := config.ModelProfile{
		Name:            "nvidia-deepseek-3.2",
		ChatModel:       "deepseek-ai/deepseek-v3.2",
		ResponsesModel:  "deepseek-ai/deepseek-v3.2",
		ReasoningEffort: "minimal",
		Tests: map[string]config.TestOverride{
			"chat.stream": {
				InstructionRole: "system",
				ReasoningEffort: "omit",
				MaxTokens:       intPtr(8),
			},
		},
	}
	client := httpclient.NewWithHTTPClient(
		cfg.BaseURL,
		"",
		nil,
		httpclient.BuildRetryConfig(1, 0, nil),
		&http.Client{
			Timeout: 5 * time.Second,
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				if err := json.Unmarshal(body, &gotBody); err != nil {
					t.Fatalf("unmarshal body: %v", err)
				}
				return stringResponse(http.StatusOK, "text/event-stream", "data: {\"choices\":[{\"delta\":{\"content\":\"HELLO\"},\"finish_reason\":\"stop\"}]}\n\n"), nil
			}),
		},
	)

	res := runChatStream(context.Background(), RunContext{
		Client:  client,
		Config:  cfg,
		Profile: profile,
	})
	if res.Status != StatusPass {
		t.Fatalf("status=%s error=%s", res.Status, res.ErrorMessage)
	}
	if _, ok := gotBody["reasoning"]; ok {
		t.Fatalf("expected reasoning to be omitted, got request body %v", gotBody["reasoning"])
	}
	if got, ok := gotBody["max_tokens"].(float64); !ok || got != 8 {
		t.Fatalf("max_tokens=%v, want 8", gotBody["max_tokens"])
	}
}

func TestRunChatBasicAppliesOverrides(t *testing.T) {
	var gotBody map[string]interface{}
	cfg := config.Config{
		BaseURL: "https://example.test",
		Endpoints: config.EndpointsConfig{
			Paths: config.EndpointsPaths{
				Chat: "/v1/chat/completions",
			},
		},
		Suite: config.SuiteConfig{
			ChatReasoning: config.Toggle{Enabled: true},
			Report:        config.ReportConfig{SnippetLimitBytes: 4096},
		},
	}
	profile := config.ModelProfile{
		Name:            "p",
		ChatModel:       "m",
		ResponsesModel:  "m",
		ReasoningEffort: "minimal",
		Tests: map[string]config.TestOverride{
			"chat.basic": {
				InstructionRole: "system",
				ReasoningEffort: "omit",
			},
		},
	}
	client := httpclient.NewWithHTTPClient(
		cfg.BaseURL,
		"",
		nil,
		httpclient.BuildRetryConfig(1, 0, nil),
		&http.Client{
			Timeout: 5 * time.Second,
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				if err := json.Unmarshal(body, &gotBody); err != nil {
					t.Fatalf("unmarshal body: %v", err)
				}
				return jsonResponse(http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`), nil
			}),
		},
	)

	res := runChatBasic(context.Background(), RunContext{Client: client, Config: cfg, Profile: profile})
	if res.Status != StatusPass {
		t.Fatalf("status=%s error=%s", res.Status, res.ErrorMessage)
	}
	if _, ok := gotBody["reasoning"]; ok {
		t.Fatalf("expected reasoning to be omitted, got %v", gotBody["reasoning"])
	}
	msgs, ok := gotBody["messages"].([]interface{})
	if !ok || len(msgs) < 1 {
		t.Fatalf("messages=%T %v", gotBody["messages"], gotBody["messages"])
	}
	first, ok := msgs[0].(map[string]interface{})
	if !ok {
		t.Fatalf("first message=%T", msgs[0])
	}
	if got := first["role"]; got != "system" {
		t.Fatalf("role=%v, want system", got)
	}
}

func TestRunResponsesBasicAppliesOverrides(t *testing.T) {
	var gotBody map[string]interface{}
	cfg := config.Config{
		BaseURL: "https://example.test",
		Endpoints: config.EndpointsConfig{
			Paths: config.EndpointsPaths{
				Responses: "/v1/responses",
			},
		},
		Suite: config.SuiteConfig{
			Report: config.ReportConfig{SnippetLimitBytes: 4096},
		},
	}
	profile := config.ModelProfile{
		Name:            "p",
		ChatModel:       "m",
		ResponsesModel:  "m",
		ReasoningEffort: "minimal",
		Tests: map[string]config.TestOverride{
			"responses.basic": {
				InstructionRole: "system",
				ReasoningEffort: "omit",
			},
		},
	}
	client := httpclient.NewWithHTTPClient(
		cfg.BaseURL,
		"",
		nil,
		httpclient.BuildRetryConfig(1, 0, nil),
		&http.Client{
			Timeout: 5 * time.Second,
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				if err := json.Unmarshal(body, &gotBody); err != nil {
					t.Fatalf("unmarshal body: %v", err)
				}
				return jsonResponse(http.StatusOK, `{"output":[{"type":"message","content":[{"type":"output_text","text":"OK"}]}]}`), nil
			}),
		},
	)

	res := runResponsesBasic(context.Background(), RunContext{Client: client, Config: cfg, Profile: profile})
	if res.Status != StatusPass {
		t.Fatalf("status=%s error=%s", res.Status, res.ErrorMessage)
	}
	if _, ok := gotBody["reasoning"]; ok {
		t.Fatalf("expected reasoning to be omitted, got %v", gotBody["reasoning"])
	}
	input, ok := gotBody["input"].([]interface{})
	if !ok || len(input) < 1 {
		t.Fatalf("input=%T %v", gotBody["input"], gotBody["input"])
	}
	first, ok := input[0].(map[string]interface{})
	if !ok {
		t.Fatalf("first input=%T", input[0])
	}
	if got := first["role"]; got != "system" {
		t.Fatalf("role=%v, want system", got)
	}
}

func TestRunResponsesStreamAppliesOverrides(t *testing.T) {
	var gotBody map[string]interface{}
	cfg := config.Config{
		BaseURL: "https://example.test",
		Endpoints: config.EndpointsConfig{
			Paths: config.EndpointsPaths{
				Responses: "/v1/responses",
			},
		},
		Suite: config.SuiteConfig{
			Report: config.ReportConfig{SnippetLimitBytes: 4096},
		},
	}
	profile := config.ModelProfile{
		Name:            "p",
		ChatModel:       "m",
		ResponsesModel:  "m",
		ReasoningEffort: "minimal",
		Tests: map[string]config.TestOverride{
			"responses.stream": {
				InstructionRole: "user",
				ReasoningEffort: "omit",
				MaxOutputTokens: intPtr(8),
			},
		},
	}
	client := httpclient.NewWithHTTPClient(
		cfg.BaseURL,
		"",
		nil,
		httpclient.BuildRetryConfig(1, 0, nil),
		&http.Client{
			Timeout: 5 * time.Second,
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				if err := json.Unmarshal(body, &gotBody); err != nil {
					t.Fatalf("unmarshal body: %v", err)
				}
				return stringResponse(http.StatusOK, "text/event-stream", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"HELLO\"}\n\n"), nil
			}),
		},
	)

	res := runResponsesStream(context.Background(), RunContext{Client: client, Config: cfg, Profile: profile})
	if res.Status != StatusPass {
		t.Fatalf("status=%s error=%s", res.Status, res.ErrorMessage)
	}
	if _, ok := gotBody["reasoning"]; ok {
		t.Fatalf("expected reasoning to be omitted, got %v", gotBody["reasoning"])
	}
	if got, ok := gotBody["max_output_tokens"].(float64); !ok || got != 8 {
		t.Fatalf("max_output_tokens=%v, want 8", gotBody["max_output_tokens"])
	}
	input, ok := gotBody["input"].([]interface{})
	if !ok || len(input) < 1 {
		t.Fatalf("input=%T %v", gotBody["input"], gotBody["input"])
	}
	first, ok := input[0].(map[string]interface{})
	if !ok {
		t.Fatalf("first input=%T", input[0])
	}
	if got := first["role"]; got != "user" {
		t.Fatalf("role=%v, want user", got)
	}
}

func TestRunChatToolCallAppliesOverrides(t *testing.T) {
	var bodies []map[string]interface{}
	cfg := config.Config{
		BaseURL: "https://example.test",
		Endpoints: config.EndpointsConfig{
			Paths: config.EndpointsPaths{
				Chat: "/v1/chat/completions",
			},
		},
		Suite: config.SuiteConfig{
			ChatReasoning: config.Toggle{Enabled: true},
			Report:        config.ReportConfig{SnippetLimitBytes: 4096},
		},
	}
	profile := config.ModelProfile{
		Name:            "p",
		ChatModel:       "m",
		ResponsesModel:  "m",
		ReasoningEffort: "minimal",
		Tests: map[string]config.TestOverride{
			"chat.tool_call": {
				ToolChoiceMode:    "forced",
				ForcedToolName:    "sum",
				ParallelToolCalls: boolPtr(true),
				ReasoningEffort:   "omit",
				MaxTokens:         intPtr(12),
				StrictMode:        boolPtr(true),
			},
		},
	}
	client := httpclient.NewWithHTTPClient(
		cfg.BaseURL,
		"",
		nil,
		httpclient.BuildRetryConfig(1, 0, nil),
		&http.Client{
			Timeout: 5 * time.Second,
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				var parsed map[string]interface{}
				if err := json.Unmarshal(body, &parsed); err != nil {
					t.Fatalf("unmarshal body: %v", err)
				}
				bodies = append(bodies, parsed)
				switch len(bodies) {
				case 1:
					return jsonResponse(http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"sum","arguments":"{\"a\":40,\"b\":2}"}}]},"finish_reason":"tool_calls"}]}`), nil
				case 2:
					return jsonResponse(http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"42"},"finish_reason":"stop"}]}`), nil
				default:
					t.Fatalf("unexpected request count: %d", len(bodies))
					return nil, nil
				}
			}),
		},
	)

	res := runChatToolCall(context.Background(), RunContext{Client: client, Config: cfg, Profile: profile})
	if res.Status != StatusPass {
		t.Fatalf("status=%s error=%s", res.Status, res.ErrorMessage)
	}
	if len(bodies) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(bodies))
	}
	first := bodies[0]
	if _, ok := first["reasoning"]; ok {
		t.Fatalf("expected reasoning to be omitted in step1, got %v", first["reasoning"])
	}
	if got, ok := first["max_tokens"].(float64); !ok || got != 12 {
		t.Fatalf("max_tokens step1=%v, want 12", first["max_tokens"])
	}
	if got, ok := first["parallel_tool_calls"].(bool); !ok || !got {
		t.Fatalf("parallel_tool_calls step1=%v, want true", first["parallel_tool_calls"])
	}
	toolChoice, ok := first["tool_choice"].(map[string]interface{})
	if !ok {
		t.Fatalf("tool_choice=%T", first["tool_choice"])
	}
	function, ok := toolChoice["function"].(map[string]interface{})
	if !ok || function["name"] != "sum" {
		t.Fatalf("tool_choice.function=%v", toolChoice["function"])
	}
	tools, ok := first["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("tools=%T %v", first["tools"], first["tools"])
	}
	tool, ok := tools[0].(map[string]interface{})
	if !ok {
		t.Fatalf("tool=%T", tools[0])
	}
	fn, ok := tool["function"].(map[string]interface{})
	if !ok {
		t.Fatalf("tool.function=%T", tool["function"])
	}
	if got := fn["name"]; got != "sum" {
		t.Fatalf("tool function name=%v", got)
	}
	if got := fn["strict"]; got != true {
		t.Fatalf("strict=%v, want true", got)
	}

	second := bodies[1]
	if _, ok := second["reasoning"]; ok {
		t.Fatalf("expected reasoning to be omitted in step2, got %v", second["reasoning"])
	}
	if got, ok := second["max_tokens"].(float64); !ok || got != 12 {
		t.Fatalf("max_tokens step2=%v, want 12", second["max_tokens"])
	}
}

func TestRunChatToolCallOmitsStrictByDefault(t *testing.T) {
	var firstBody map[string]interface{}
	cfg := config.Config{
		BaseURL: "https://example.test",
		Endpoints: config.EndpointsConfig{
			Paths: config.EndpointsPaths{
				Chat: "/v1/chat/completions",
			},
		},
		Suite: config.SuiteConfig{
			ChatReasoning: config.Toggle{Enabled: true},
			Report:        config.ReportConfig{SnippetLimitBytes: 4096},
		},
	}
	profile := config.ModelProfile{
		Name:           "p",
		ChatModel:      "m",
		ResponsesModel: "m",
	}
	client := httpclient.NewWithHTTPClient(
		cfg.BaseURL,
		"",
		nil,
		httpclient.BuildRetryConfig(1, 0, nil),
		&http.Client{
			Timeout: 5 * time.Second,
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				if firstBody == nil {
					if err := json.Unmarshal(body, &firstBody); err != nil {
						t.Fatalf("unmarshal body: %v", err)
					}
					return jsonResponse(http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"add","arguments":"{\"a\":40,\"b\":2}"}}]},"finish_reason":"tool_calls"}]}`), nil
				}
				return jsonResponse(http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"42"},"finish_reason":"stop"}]}`), nil
			}),
		},
	)

	res := runChatToolCall(context.Background(), RunContext{Client: client, Config: cfg, Profile: profile})
	if res.Status != StatusPass {
		t.Fatalf("status=%s error=%s", res.Status, res.ErrorMessage)
	}
	tools, ok := firstBody["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("tools=%T %v", firstBody["tools"], firstBody["tools"])
	}
	tool, ok := tools[0].(map[string]interface{})
	if !ok {
		t.Fatalf("tool=%T", tools[0])
	}
	fn, ok := tool["function"].(map[string]interface{})
	if !ok {
		t.Fatalf("tool.function=%T", tool["function"])
	}
	if _, ok := fn["strict"]; ok {
		t.Fatalf("did not expect strict to be serialized when false")
	}
}

func intPtr(v int) *int {
	return &v
}

func boolPtr(v bool) *bool {
	return &v
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonResponse(status int, body string) *http.Response {
	return stringResponse(status, "application/json", body)
}

func stringResponse(status int, contentType, body string) *http.Response {
	resp := &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	if contentType != "" {
		resp.Header.Set("Content-Type", contentType)
	}
	return resp
}
