package tests

import (
	"strings"
	"testing"
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

func TestParseChatStreamEvent(t *testing.T) {
	data := `{"choices":[{"delta":{"content":"Hi"}}]}`
	delta, done := parseChatStreamEvent(data)
	if delta != "Hi" || done {
		t.Fatalf("delta=%q done=%v", delta, done)
	}
	_, done = parseChatStreamEvent("[DONE]")
	if !done {
		t.Fatalf("expected done")
	}
}

func TestParseResponsesStreamEvent(t *testing.T) {
	data := `{"type":"response.output_text.delta","delta":{"text":"OK"}}`
	delta, done := parseResponsesStreamEvent(data)
	if delta != "OK" || done {
		t.Fatalf("delta=%q done=%v", delta, done)
	}
	_, done = parseResponsesStreamEvent("[DONE]")
	if !done {
		t.Fatalf("expected done")
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
