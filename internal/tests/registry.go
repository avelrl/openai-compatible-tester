package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/avelrl/openai-compatible-tester/internal/config"
	"github.com/avelrl/openai-compatible-tester/internal/httpclient"
	"github.com/avelrl/openai-compatible-tester/internal/sse"
)

type Status string

const (
	StatusQueued      Status = "queued"
	StatusRunning     Status = "running"
	StatusPass        Status = "pass"
	StatusFail        Status = "fail"
	StatusTimeout     Status = "timeout"
	StatusUnsupported Status = "unsupported"
)

var snippetLimit = 4096

type TraceStep struct {
	Name     string `json:"name"`
	Request  string `json:"request,omitempty"`
	Response string `json:"response,omitempty"`
}

type Result struct {
	TestID               string
	TestName             string
	Status               Status
	HTTPStatus           int
	LatencyMS            int64
	BytesIn              int64
	BytesOut             int64
	Tokens               int
	ErrorType            string
	ErrorMessage         string
	Profile              string
	Pass                 int
	Attempts             int
	Model                string
	RequestSnippet       string
	ResponseSnippet      string
	TraceSteps           []TraceStep
	ToolChoiceMode       string
	ReasoningEffort      string
	LiteLLMTimeout       string
	FunctionCallObserved bool
	IsWarmup             bool
	snippetLimit         int
}

type TestCase struct {
	ID       string
	Name     string
	Category string

	RequiresStream        bool
	RequiresTools         bool
	RequiresStructured    bool
	RequiresConversations bool
	RequiresMemory        bool
	Kind                  string // responses, chat, sanity

	Run func(ctx context.Context, rc RunContext) Result
}

type RunContext struct {
	Client   *httpclient.Client
	Config   config.Config
	Profile  config.ModelProfile
	Pass     int
	IsWarmup bool
}

const (
	KindResponses = "responses"
	KindChat      = "chat"
	KindSanity    = "sanity"
)

func Registry() []TestCase {
	return []TestCase{
		{
			ID:       "sanity.models",
			Name:     "GET /v1/models",
			Category: "sanity",
			Kind:     KindSanity,
			Run:      runModelsList,
		},
		{
			ID:       "responses.basic",
			Name:     "Responses basic",
			Category: "responses",
			Kind:     KindResponses,
			Run:      runResponsesBasic,
		},
		{
			ID:       "responses.store_get",
			Name:     "Responses store + GET",
			Category: "responses",
			Kind:     KindResponses,
			Run:      runResponsesStoreGet,
		},
		{
			ID:             "responses.stream",
			Name:           "Responses streaming",
			Category:       "responses",
			Kind:           KindResponses,
			RequiresStream: true,
			Run:            runResponsesStream,
		},
		{
			ID:                 "responses.structured.json_schema",
			Name:               "Responses structured json_schema",
			Category:           "responses",
			Kind:               KindResponses,
			RequiresStructured: true,
			Run:                runResponsesStructuredSchema,
		},
		{
			ID:                 "responses.structured.json_object",
			Name:               "Responses structured json_object",
			Category:           "responses",
			Kind:               KindResponses,
			RequiresStructured: true,
			Run:                runResponsesStructuredObject,
		},
		{
			ID:            "responses.tool_call",
			Name:          "Responses tool calling",
			Category:      "responses",
			Kind:          KindResponses,
			RequiresTools: true,
			Run:           runResponsesToolCall,
		},
		{
			ID:            "responses.tool_call.required",
			Name:          "Responses tool calling (required)",
			Category:      "responses",
			Kind:          KindResponses,
			RequiresTools: true,
			Run:           runResponsesToolCallRequired,
		},
		{
			ID:       "responses.error_shape",
			Name:     "Responses error shape",
			Category: "responses",
			Kind:     KindResponses,
			Run:      runResponsesErrorShape,
		},
		{
			ID:             "responses.memory.prev_id",
			Name:           "Responses memory (previous_response_id)",
			Category:       "responses",
			Kind:           KindResponses,
			RequiresMemory: true,
			Run:            runResponsesMemory,
		},
		{
			ID:                    "responses.conversations",
			Name:                  "Responses conversations",
			Category:              "responses",
			Kind:                  KindResponses,
			RequiresConversations: true,
			Run:                   runResponsesConversations,
		},
		{
			ID:       "chat.basic",
			Name:     "Chat completions basic",
			Category: "chat",
			Kind:     KindChat,
			Run:      runChatBasic,
		},
		{
			ID:             "chat.stream",
			Name:           "Chat completions streaming",
			Category:       "chat",
			Kind:           KindChat,
			RequiresStream: true,
			Run:            runChatStream,
		},
		{
			ID:            "chat.tool_call",
			Name:          "Chat tool calling",
			Category:      "chat",
			Kind:          KindChat,
			RequiresTools: true,
			Run:           runChatToolCall,
		},
		{
			ID:            "chat.tool_call.required",
			Name:          "Chat tool calling (required)",
			Category:      "chat",
			Kind:          KindChat,
			RequiresTools: true,
			Run:           runChatToolCallRequired,
		},
		{
			ID:       "chat.error_shape",
			Name:     "Chat error shape",
			Category: "chat",
			Kind:     KindChat,
			Run:      runChatErrorShape,
		},
		{
			ID:             "chat.memory",
			Name:           "Chat memory",
			Category:       "chat",
			Kind:           KindChat,
			RequiresMemory: true,
			Run:            runChatMemory,
		},
		{
			ID:                 "chat.structured.json_schema",
			Name:               "Chat structured json_schema",
			Category:           "chat",
			Kind:               KindChat,
			RequiresStructured: true,
			Run:                runChatStructuredSchema,
		},
		{
			ID:                 "chat.structured.json_object",
			Name:               "Chat structured json_object",
			Category:           "chat",
			Kind:               KindChat,
			RequiresStructured: true,
			Run:                runChatStructuredObject,
		},
	}
}

func baseResponsesPayload(profile config.ModelProfile) map[string]interface{} {
	payload := map[string]interface{}{
		"model": profile.ResponsesModel,
	}
	if profile.Temperature != nil {
		payload["temperature"] = *profile.Temperature
	}
	if profile.ReasoningEffort != "" {
		payload["reasoning"] = map[string]interface{}{"effort": profile.ReasoningEffort}
	}
	for k, v := range profile.Extra {
		payload[k] = v
	}
	return payload
}

func baseChatPayload(profile config.ModelProfile, suite config.SuiteConfig) map[string]interface{} {
	payload := map[string]interface{}{
		"model": profile.ChatModel,
	}
	if profile.Temperature != nil {
		payload["temperature"] = *profile.Temperature
	}
	// Chat "reasoning" is non-standard; only send when explicitly enabled.
	if suite.ChatReasoning.Enabled && profile.ReasoningEffort != "" {
		payload["reasoning"] = map[string]interface{}{"effort": profile.ReasoningEffort}
	}
	for k, v := range profile.Extra {
		payload[k] = v
	}
	return payload
}

func runModelsList(ctx context.Context, rc RunContext) Result {
	result := baseResult("sanity.models", "GET /v1/models", rc)
	headers := requestHeadersForTest(rc.Config, "sanity.models")
	resp, err := rc.Client.Get(ctx, rc.Config.Endpoints.Paths.Models, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	result.HTTPStatus = resp.StatusCode
	result.LatencyMS = resp.Latency.Milliseconds()
	result.BytesIn = resp.BytesIn
	result.BytesOut = resp.BytesOut
	result.ResponseSnippet = clip(string(resp.Body), snippetLimit)
	if resp.StatusCode == 404 || resp.StatusCode == 405 {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return failHTTPStatusResult(result, resp)
	}
	if !isModelsListLike(resp.Body) {
		return failResult(result, errors.New("response not list-like"), "schema")
	}
	result.Status = StatusPass
	return result
}

func runResponsesBasic(ctx context.Context, rc RunContext) Result {
	result := baseResult("responses.basic", "Responses basic", rc)
	headers := requestHeadersForTest(rc.Config, "responses.basic")
	payload := baseResponsesPayload(rc.Profile)
	payload["input"] = "ping: answer with OK"
	result.RequestSnippet = clip(prettyJSON(payload), snippetLimit)
	resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	return handleResponsesTextResult(result, resp, "OK")
}

func runResponsesStoreGet(ctx context.Context, rc RunContext) Result {
	result := baseResult("responses.store_get", "Responses store + GET", rc)
	headers := requestHeadersForTest(rc.Config, "responses.store_get")
	payload := baseResponsesPayload(rc.Profile)
	payload["input"] = "Say OK and nothing else"
	payload["store"] = true
	step1Req := prettyJSON(payload)
	result.RequestSnippet = clip(step1Req, snippetLimit)
	withTraceStep(&result, "create_response", step1Req, "")
	resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	result.HTTPStatus = resp.StatusCode
	result.LatencyMS = resp.Latency.Milliseconds()
	result.BytesIn = resp.BytesIn
	result.BytesOut = resp.BytesOut
	step1RespText := string(resp.Body)
	result.ResponseSnippet = clip(step1RespText, snippetLimit)
	updateTraceStepResponse(&result, "create_response", step1RespText)
	if isEndpointMissing(resp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if isUnsupportedFeature(resp.Body) {
		return unsupportedResult(result, "unknown_parameter", errorMessage(resp.Body))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return failHTTPStatusResult(result, resp)
	}
	id := extractResponseID(resp.Body)
	if id == "" {
		return failResult(result, errors.New("missing id"), "schema")
	}
	if strings.TrimSpace(extractResponseText(resp.Body)) == "" {
		return failResult(result, errors.New("missing text"), "schema")
	}
	getResp, err := rc.Client.Get(ctx, rc.Config.Endpoints.Paths.Responses+"/"+id, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	step2RespText := string(getResp.Body)
	withTraceStep(&result, "get_response", "GET "+rc.Config.Endpoints.Paths.Responses+"/"+id, step2RespText)
	result.RequestSnippet = clip("STEP1:\n"+step1Req+"\n\nSTEP2:\nGET "+rc.Config.Endpoints.Paths.Responses+"/"+id, snippetLimit)
	result.ResponseSnippet = clip("STEP1:\n"+step1RespText+"\n\nSTEP2:\n"+step2RespText, snippetLimit)
	if isGetUnsupported(getResp.Body) || isUnexpectedEndpointOrMethod(getResp.Body) {
		result.HTTPStatus = getResp.StatusCode
		result.LatencyMS = getResp.Latency.Milliseconds()
		result.BytesIn = getResp.BytesIn
		result.BytesOut = getResp.BytesOut
		return unsupportedResult(result, "unsupported_get", errorMessage(getResp.Body))
	}
	if getResp.StatusCode < 200 || getResp.StatusCode >= 300 {
		result.HTTPStatus = getResp.StatusCode
		result.LatencyMS = getResp.Latency.Milliseconds()
		result.BytesIn = getResp.BytesIn
		result.BytesOut = getResp.BytesOut
		if isEndpointMissing(getResp.StatusCode) {
			return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", getResp.StatusCode))
		}
		if isGetUnsupported(getResp.Body) {
			return unsupportedResult(result, "unsupported_get", errorMessage(getResp.Body))
		}
		return failHTTPStatusResult(result, getResp)
	}
	if strings.TrimSpace(extractResponseText(getResp.Body)) == "" {
		return failResult(result, errors.New("missing text"), "schema")
	}
	if gotID := extractResponseID(getResp.Body); gotID != "" && gotID != id {
		return failResult(result, fmt.Errorf("id mismatch: %s != %s", gotID, id), "schema")
	}
	result.Status = StatusPass
	return result
}

func runResponsesStructuredSchema(ctx context.Context, rc RunContext) Result {
	result := baseResult("responses.structured.json_schema", "Responses structured json_schema", rc)
	headers := requestHeadersForTest(rc.Config, "responses.structured.json_schema")
	payload := baseResponsesPayload(rc.Profile)
	payload["input"] = []map[string]string{
		{"role": "system", "content": "Return JSON strictly according to the schema."},
		{"role": "user", "content": "Generate an object with status ok and the number 42."},
	}
	payload["text"] = map[string]interface{}{
		"format": map[string]interface{}{
			"type": "json_schema",
			"name": "simple_status",
			"schema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"status": map[string]interface{}{"type": "string"},
					"value":  map[string]interface{}{"type": "integer"},
				},
				"required":             []string{"status", "value"},
				"additionalProperties": false,
			},
			"strict": true,
		},
	}
	result.RequestSnippet = clip(prettyJSON(payload), snippetLimit)
	resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	return handleStructuredResponse(result, resp, true)
}

func runResponsesStructuredObject(ctx context.Context, rc RunContext) Result {
	result := baseResult("responses.structured.json_object", "Responses structured json_object", rc)
	headers := requestHeadersForTest(rc.Config, "responses.structured.json_object")
	payload := baseResponsesPayload(rc.Profile)
	payload["input"] = []map[string]string{
		{"role": "system", "content": "You output JSON only."},
		{"role": "user", "content": "Return JSON like {\"ok\":true}."},
	}
	payload["text"] = map[string]interface{}{
		"format": map[string]interface{}{"type": "json_object"},
	}
	result.RequestSnippet = clip(prettyJSON(payload), snippetLimit)
	resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	return handleStructuredResponse(result, resp, false)
}

func runResponsesToolCall(ctx context.Context, rc RunContext) Result {
	return runResponsesToolCallVariant(ctx, rc, "responses.tool_call", "Responses tool calling", "")
}

func runResponsesToolCallRequired(ctx context.Context, rc RunContext) Result {
	return runResponsesToolCallVariant(ctx, rc, "responses.tool_call.required", "Responses tool calling (required)", "required")
}

func runResponsesToolCallVariant(ctx context.Context, rc RunContext, testID, name, forcedMode string) Result {
	result := baseResult(testID, name, rc)
	headers := requestHeadersForTest(rc.Config, testID)
	ov, _ := testOverride(rc.Config, testID)

	mode := strings.TrimSpace(ov.ToolChoiceMode)
	if forcedMode != "" {
		mode = forcedMode
	}
	if mode == "" {
		mode = "forced"
	}
	toolName := strings.TrimSpace(ov.ForcedToolName)
	if toolName == "" {
		toolName = "add"
	}
	reasoningEffort := strings.TrimSpace(ov.ReasoningEffort)
	if reasoningEffort == "" {
		reasoningEffort = "minimal"
	}
	parallel := false
	if ov.ParallelToolCalls != nil {
		parallel = *ov.ParallelToolCalls
	}
	strict := false
	if ov.StrictMode != nil {
		strict = *ov.StrictMode
	}
	var maxOutputTokens *int
	if ov.MaxOutputTokens != nil {
		mt := *ov.MaxOutputTokens
		if mt > 64 {
			mt = 64
		}
		if mt > 0 {
			maxOutputTokens = &mt
		}
	}
	useStream := false
	if rc.Config.Suite.Stream.Enabled && ov.Stream != nil {
		useStream = *ov.Stream
	}

	result.ToolChoiceMode = mode
	result.ReasoningEffort = reasoningEffort
	result.LiteLLMTimeout = effectiveHeaderValueForTest(rc.Config, testID, "x-litellm-timeout")

	payload := baseResponsesPayload(rc.Profile)
	switch mode {
	case "forced":
		payload["tool_choice"] = map[string]interface{}{"type": "function", "name": toolName}
	case "required":
		payload["tool_choice"] = "required"
	case "auto":
		// omit tool_choice (auto is the default on most servers)
	default:
		payload["tool_choice"] = map[string]interface{}{"type": "function", "name": toolName}
	}
	payload["parallel_tool_calls"] = parallel
	if maxOutputTokens != nil {
		payload["max_output_tokens"] = *maxOutputTokens
	}
	if reasoningEffort == "omit" {
		delete(payload, "reasoning")
	} else {
		payload["reasoning"] = map[string]interface{}{"effort": reasoningEffort}
	}
	payload["tools"] = []map[string]interface{}{
		{
			"type":        "function",
			"name":        toolName,
			"description": "Add two integers",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"a": map[string]interface{}{"type": "integer"},
					"b": map[string]interface{}{"type": "integer"},
				},
				"required":             []string{"a", "b"},
				"additionalProperties": false,
			},
			"strict": strict,
		},
	}
	step1Prompt := fmt.Sprintf("Call %s with a=40 and b=2. Do not answer yourself.", toolName)
	step2Prompt := "Reply with just the number."
	payload["input"] = []map[string]string{
		{"role": "user", "content": step1Prompt},
	}

	if useStream {
		payload["stream"] = true
	}
	step1Req := prettyJSON(payload)
	result.RequestSnippet = clip(step1Req, snippetLimit)
	withTraceStep(&result, "request_tool_call", step1Req, "")

	var (
		callID        string
		callArgs      string
		step1Body     []byte
		step1RespText string
	)

	if useStream {
		streamHeaders := cloneHeaders(headers)
		streamHeaders["Accept"] = "text/event-stream"
		if effectiveHeaderValueForTest(rc.Config, testID, "x-litellm-stream-timeout") == "" {
			streamTimeoutSeconds := ov.StreamTimeoutSeconds
			if streamTimeoutSeconds <= 0 {
				streamTimeoutSeconds = 30
			}
			streamHeaders["x-litellm-stream-timeout"] = strconv.Itoa(streamTimeoutSeconds)
		}

		var raw strings.Builder
		argsBuf := ""
		resp, err := rc.Client.PostJSONStream(ctx, rc.Config.Endpoints.Paths.Responses, payload, streamHeaders, func(ev sse.Event) error {
			if ev.Data == "" {
				return nil
			}
			raw.WriteString(ev.Data)
			raw.WriteString("\n")

			id, _, argsPiece := parseResponsesToolCallStreamEvent(ev.Data)
			if id != "" {
				callID = id
			}
			if argsPiece != "" {
				argsBuf = mergeArgsJSON(argsBuf, argsPiece)
			}
			if callID != "" && isValidJSON(argsBuf) {
				callArgs = argsBuf
				result.FunctionCallObserved = true
				return sse.ErrStop
			}
			return nil
		})
		if err != nil {
			result.ResponseSnippet = clip(raw.String(), snippetLimit)
			return failResult(result, err, "http_error")
		}
		result.HTTPStatus = resp.StatusCode
		result.LatencyMS = resp.Latency.Milliseconds()
		result.BytesIn = resp.BytesIn
		result.BytesOut = resp.BytesOut
		step1Body = resp.Body
		step1RespText = raw.String()
		result.ResponseSnippet = clip(step1RespText, snippetLimit)
		updateTraceStepResponse(&result, "request_tool_call", step1RespText)

		if isEndpointMissing(resp.StatusCode) {
			return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
		}
		if resp.StatusCode >= 400 && isUnsupportedFeature(resp.Body) {
			return unsupportedResult(result, "unknown_parameter", errorMessage(resp.Body))
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return failHTTPStatusResult(result, resp)
		}
		if callID == "" {
			return failResult(result, errors.New("missing function call"), "tool_call")
		}
		if callArgs == "" {
			callArgs = argsBuf
		}
		if strings.TrimSpace(callArgs) == "" {
			return failResult(result, errors.New("missing tool call arguments"), "tool_call")
		}
		if err := validateAddToolCallArgs(callArgs, 40, 2); err != nil {
			return failResult(result, err, "tool_call")
		}
	} else {
		resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, payload, headers)
		if err != nil {
			return failResult(result, err, "http_error")
		}
		result.HTTPStatus = resp.StatusCode
		result.LatencyMS = resp.Latency.Milliseconds()
		result.BytesIn = resp.BytesIn
		result.BytesOut = resp.BytesOut
		step1Body = resp.Body
		step1RespText = string(resp.Body)
		result.ResponseSnippet = clip(step1RespText, snippetLimit)
		updateTraceStepResponse(&result, "request_tool_call", step1RespText)

		if isEndpointMissing(resp.StatusCode) {
			return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
		}
		if isUnsupportedFeature(resp.Body) {
			return unsupportedResult(result, "unknown_parameter", errorMessage(resp.Body))
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return failHTTPStatusResult(result, resp)
		}
		callID, callArgs = extractResponseFunctionCall(resp.Body)
		if callID == "" {
			return failResult(result, errors.New("missing function call"), "tool_call")
		}
		if err := validateAddToolCallArgs(callArgs, 40, 2); err != nil {
			return failResult(result, err, "tool_call")
		}
		result.FunctionCallObserved = true
	}

	second := baseResponsesPayload(rc.Profile)
	second["tools"] = payload["tools"]
	if maxOutputTokens != nil {
		second["max_output_tokens"] = *maxOutputTokens
	}
	if reasoningEffort == "omit" {
		delete(second, "reasoning")
	} else {
		second["reasoning"] = map[string]interface{}{"effort": reasoningEffort}
	}

	var input []interface{}
	if useStream {
		// We stopped reading early, so reconstruct a stateless follow-up with the original user turn.
		input = buildResponsesToolFollowupInput(nil, callID, callArgs, toolName, step1Prompt, step2Prompt, "{\"result\":42}")
	} else {
		input = buildResponsesToolFollowupInput(step1Body, callID, callArgs, toolName, step1Prompt, step2Prompt, "{\"result\":42}")
	}
	second["input"] = input
	step2Req := prettyJSON(second)
	withTraceStep(&result, "submit_tool_output", step2Req, "")

	resp2, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, second, headers)
	if err != nil {
		result.RequestSnippet = clip("STEP1:\n"+step1Req+"\n\nSTEP2:\n"+step2Req, snippetLimit)
		result.ResponseSnippet = clip("STEP1:\n"+step1RespText, snippetLimit)
		return failResult(result, err, "http_error")
	}

	step2RespText := string(resp2.Body)
	result.RequestSnippet = clip("STEP1:\n"+step1Req+"\n\nSTEP2:\n"+step2Req, snippetLimit)
	tmp := handleResponsesTextResultAllowRepeatedScalar(result, resp2, "42")
	tmp.ResponseSnippet = clip("STEP1:\n"+step1RespText+"\n\nSTEP2:\n"+step2RespText, snippetLimit)
	updateTraceStepResponse(&tmp, "submit_tool_output", step2RespText)
	return tmp
}

func runResponsesErrorShape(ctx context.Context, rc RunContext) Result {
	result := baseResult("responses.error_shape", "Responses error shape", rc)
	headers := requestHeadersForTest(rc.Config, "responses.error_shape")
	payload := map[string]interface{}{
		"model": rc.Profile.ResponsesModel,
		"input": 1, // invalid input type
	}
	result.RequestSnippet = clip(prettyJSON(payload), snippetLimit)
	resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	result.HTTPStatus = resp.StatusCode
	result.LatencyMS = resp.Latency.Milliseconds()
	result.BytesIn = resp.BytesIn
	result.BytesOut = resp.BytesOut
	result.ResponseSnippet = clip(string(resp.Body), snippetLimit)
	if isUnexpectedEndpointOrMethod(resp.Body) {
		return unsupportedResult(result, "endpoint_missing", errorMessage(resp.Body))
	}
	if isEndpointMissing(resp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if resp.StatusCode < 400 {
		return failResult(result, fmt.Errorf("expected error status, got %d", resp.StatusCode), "expected_error")
	}
	if resp.StatusCode >= 500 {
		// For an invalid request, a 5xx usually means the proxy/backend crashed instead of returning a validation error.
		return failResult(result, fmt.Errorf("expected 4xx client error, got %d", resp.StatusCode), "server_error")
	}
	if strings.TrimSpace(errorMessage(resp.Body)) == "" {
		return failResult(result, errors.New("missing error message"), "schema")
	}
	result.Status = StatusPass
	return result
}

func runResponsesMemory(ctx context.Context, rc RunContext) Result {
	result := baseResult("responses.memory.prev_id", "Responses memory (previous_response_id)", rc)
	headers := requestHeadersForTest(rc.Config, "responses.memory.prev_id")
	payload := baseResponsesPayload(rc.Profile)
	payload["input"] = "Remember: my code = 123. Reply OK"
	result.RequestSnippet = clip(prettyJSON(payload), snippetLimit)
	withTraceStep(&result, "remember_value", prettyJSON(payload), "")
	resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	updateTraceStepResponse(&result, "remember_value", string(resp.Body))
	if isEndpointMissing(resp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return failHTTPStatusResult(result, resp)
	}
	id := extractResponseID(resp.Body)
	if id == "" {
		return failResult(result, errors.New("missing id"), "schema")
	}
	payload2 := baseResponsesPayload(rc.Profile)
	payload2["previous_response_id"] = id
	payload2["input"] = "What was my code? Reply with just the number."
	result.RequestSnippet = clip(prettyJSON(payload2), snippetLimit)
	withTraceStep(&result, "follow_up_with_previous_response_id", prettyJSON(payload2), "")
	resp2, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, payload2, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	updateTraceStepResponse(&result, "follow_up_with_previous_response_id", string(resp2.Body))
	if extractPreviousResponseID(resp2.Body) == "" {
		result.HTTPStatus = resp2.StatusCode
		result.LatencyMS = resp2.Latency.Milliseconds()
		result.BytesIn = resp2.BytesIn
		result.BytesOut = resp2.BytesOut
		result.ResponseSnippet = clip(string(resp2.Body), snippetLimit)
		return unsupportedResult(result, "stateless_responses", "previous_response_id was ignored by provider")
	}
	return handleResponsesTextResult(result, resp2, "123")
}

func runResponsesConversations(ctx context.Context, rc RunContext) Result {
	result := baseResult("responses.conversations", "Responses conversations", rc)
	headers := requestHeadersForTest(rc.Config, "responses.conversations")
	payload := map[string]interface{}{
		"items": []map[string]string{
			{"type": "message", "role": "system", "content": "You are a test assistant."},
			{"type": "message", "role": "user", "content": "Remember: code=777. Reply OK."},
		},
	}
	result.RequestSnippet = clip(prettyJSON(payload), snippetLimit)
	withTraceStep(&result, "create_conversation", prettyJSON(payload), "")
	resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Conversations, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	result.HTTPStatus = resp.StatusCode
	result.LatencyMS = resp.Latency.Milliseconds()
	result.BytesIn = resp.BytesIn
	result.BytesOut = resp.BytesOut
	result.ResponseSnippet = clip(string(resp.Body), snippetLimit)
	updateTraceStepResponse(&result, "create_conversation", string(resp.Body))
	if isHTMLDocument(resp.Body) {
		return unsupportedResult(result, "endpoint_missing", "html page returned instead of API response")
	}
	if isUnexpectedEndpointOrMethod(resp.Body) {
		return unsupportedResult(result, "endpoint_missing", errorMessage(resp.Body))
	}
	if isEndpointMissing(resp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return failHTTPStatusResult(result, resp)
	}
	convID := extractResponseID(resp.Body)
	if convID == "" {
		return failResult(result, errors.New("missing conversation id"), "schema")
	}
	payload2 := baseResponsesPayload(rc.Profile)
	payload2["conversation"] = convID
	payload2["input"] = []map[string]string{{"role": "user", "content": "What is the code? Reply with just the number."}}
	result.RequestSnippet = clip(prettyJSON(payload2), snippetLimit)
	withTraceStep(&result, "use_conversation", prettyJSON(payload2), "")
	resp2, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, payload2, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	updateTraceStepResponse(&result, "use_conversation", string(resp2.Body))
	return handleResponsesTextResult(result, resp2, "777")
}

func runChatBasic(ctx context.Context, rc RunContext) Result {
	result := baseResult("chat.basic", "Chat completions basic", rc)
	result.Model = rc.Profile.ChatModel
	headers := requestHeadersForTest(rc.Config, "chat.basic")
	payload := baseChatPayload(rc.Profile, rc.Config.Suite)
	payload["messages"] = []map[string]string{
		{"role": "developer", "content": "Reply with exactly OK"},
		{"role": "user", "content": "ping"},
	}
	result.RequestSnippet = clip(prettyJSON(payload), snippetLimit)
	resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Chat, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	return handleChatTextResult(result, resp, "OK")
}

func runChatStream(ctx context.Context, rc RunContext) Result {
	result := baseResult("chat.stream", "Chat completions streaming", rc)
	result.Model = rc.Profile.ChatModel
	headers := cloneHeaders(requestHeadersForTest(rc.Config, "chat.stream"))
	headers["Accept"] = "text/event-stream"
	payload := baseChatPayload(rc.Profile, rc.Config.Suite)
	payload["stream"] = true
	payload["messages"] = []map[string]string{
		{"role": "developer", "content": "Stream one word: HELLO"},
		{"role": "user", "content": "go"},
	}
	result.RequestSnippet = clip(prettyJSON(payload), snippetLimit)
	var text strings.Builder
	var raw strings.Builder
	done := false
	resp, err := rc.Client.PostJSONStream(ctx, rc.Config.Endpoints.Paths.Chat, payload, headers, func(ev sse.Event) error {
		if ev.Data == "" {
			return nil
		}
		raw.WriteString(ev.Data)
		raw.WriteString("\n")
		delta, isDone := parseChatStreamEvent(ev.Data)
		if delta != "" {
			text.WriteString(delta)
		}
		if isDone {
			done = true
		}
		return nil
	})
	if err != nil {
		result.ResponseSnippet = clip("SSE:\n"+raw.String()+"\nTEXT:\n"+text.String(), snippetLimit)
		return failResult(result, err, "http_error")
	}
	result.HTTPStatus = resp.StatusCode
	result.LatencyMS = resp.Latency.Milliseconds()
	result.BytesIn = resp.BytesIn
	result.BytesOut = resp.BytesOut
	result.ResponseSnippet = clip("SSE:\n"+raw.String()+"\nTEXT:\n"+text.String(), snippetLimit)
	if isEndpointMissing(resp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if isUnsupportedFeature(resp.Body) {
		return unsupportedResult(result, "unknown_parameter", errorMessage(resp.Body))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return failHTTPStatusResult(result, resp)
	}
	if !done {
		return failResult(result, errors.New("stream did not terminate"), "stream")
	}
	if strings.TrimSpace(text.String()) == "" {
		return failResult(result, errors.New("empty stream"), "stream")
	}
	result.Status = StatusPass
	return result
}

func runChatToolCall(ctx context.Context, rc RunContext) Result {
	return runChatToolCallVariant(ctx, rc, "chat.tool_call", "Chat tool calling", "")
}

func runChatToolCallRequired(ctx context.Context, rc RunContext) Result {
	return runChatToolCallVariant(ctx, rc, "chat.tool_call.required", "Chat tool calling (required)", "required")
}

func runChatToolCallVariant(ctx context.Context, rc RunContext, testID, name, forcedMode string) Result {
	result := baseResult(testID, name, rc)
	result.Model = rc.Profile.ChatModel
	headers := requestHeadersForTest(rc.Config, testID)
	ov, _ := testOverride(rc.Config, testID)

	mode := strings.TrimSpace(ov.ToolChoiceMode)
	if forcedMode != "" {
		mode = forcedMode
	}
	if mode == "" {
		mode = "forced"
	}
	toolName := strings.TrimSpace(ov.ForcedToolName)
	if toolName == "" {
		toolName = "add"
	}
	parallel := false
	if ov.ParallelToolCalls != nil {
		parallel = *ov.ParallelToolCalls
	}
	strict := false
	if ov.StrictMode != nil {
		strict = *ov.StrictMode
	}
	maxTokens := 64
	if ov.MaxTokens != nil && *ov.MaxTokens > 0 {
		maxTokens = *ov.MaxTokens
	} else if ov.MaxOutputTokens != nil && *ov.MaxOutputTokens > 0 {
		// Convenience: allow using max_output_tokens for chat, too.
		maxTokens = *ov.MaxOutputTokens
	}
	if maxTokens > 64 {
		maxTokens = 64
	}

	result.ToolChoiceMode = mode
	// Chat "reasoning" is feature-gated; treat it as omitted unless enabled.
	if strings.TrimSpace(ov.ReasoningEffort) != "" {
		result.ReasoningEffort = strings.TrimSpace(ov.ReasoningEffort)
	} else {
		result.ReasoningEffort = "omit"
	}
	result.LiteLLMTimeout = effectiveHeaderValueForTest(rc.Config, testID, "x-litellm-timeout")

	step1Prompt := fmt.Sprintf("Call %s with a=40 and b=2. Do not answer yourself.", toolName)
	step2Prompt := "Reply with just the number."

	payload := baseChatPayload(rc.Profile, rc.Config.Suite)
	switch mode {
	case "forced":
		payload["tool_choice"] = map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name": toolName,
			},
		}
	case "required":
		payload["tool_choice"] = "required"
	case "auto":
		// omit tool_choice
	default:
		payload["tool_choice"] = map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name": toolName,
			},
		}
	}
	payload["parallel_tool_calls"] = parallel
	payload["max_tokens"] = maxTokens
	payload["tools"] = []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        toolName,
				"description": "Add two integers",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"a": map[string]interface{}{"type": "integer"},
						"b": map[string]interface{}{"type": "integer"},
					},
					"required":             []string{"a", "b"},
					"additionalProperties": false,
				},
				"strict": strict,
			},
		},
	}
	payload["messages"] = []map[string]string{{"role": "user", "content": step1Prompt}}
	step1Req := prettyJSON(payload)
	result.RequestSnippet = clip(step1Req, snippetLimit)
	withTraceStep(&result, "request_tool_call", step1Req, "")

	resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Chat, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	result.HTTPStatus = resp.StatusCode
	result.LatencyMS = resp.Latency.Milliseconds()
	result.BytesIn = resp.BytesIn
	result.BytesOut = resp.BytesOut
	step1RespText := string(resp.Body)
	result.ResponseSnippet = clip(step1RespText, snippetLimit)
	updateTraceStepResponse(&result, "request_tool_call", step1RespText)
	if isEndpointMissing(resp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if isUnsupportedFeature(resp.Body) {
		return unsupportedResult(result, "unknown_parameter", errorMessage(resp.Body))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return failHTTPStatusResult(result, resp)
	}
	callID, callArgs := extractChatToolCall(resp.Body)
	if callID == "" {
		return failResult(result, errors.New("missing tool call"), "tool_call")
	}
	if err := validateAddToolCallArgs(callArgs, 40, 2); err != nil {
		return failResult(result, err, "tool_call")
	}
	result.FunctionCallObserved = true

	payload2 := baseChatPayload(rc.Profile, rc.Config.Suite)
	payload2["parallel_tool_calls"] = parallel
	payload2["max_tokens"] = maxTokens
	payload2["tools"] = payload["tools"]
	payload2["messages"] = []interface{}{
		map[string]interface{}{"role": "user", "content": step1Prompt},
		// embed tool calls message
		chatToolCallMessage(resp.Body),
		map[string]interface{}{"role": "tool", "tool_call_id": callID, "content": "{\"result\":42}"},
		map[string]interface{}{"role": "user", "content": step2Prompt},
	}
	step2Req := prettyJSON(payload2)
	withTraceStep(&result, "submit_tool_result", step2Req, "")

	resp2, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Chat, payload2, headers)
	if err != nil {
		result.RequestSnippet = clip("STEP1:\n"+step1Req+"\n\nSTEP2:\n"+step2Req, snippetLimit)
		result.ResponseSnippet = clip("STEP1:\n"+step1RespText, snippetLimit)
		return failResult(result, err, "http_error")
	}

	step2RespText := string(resp2.Body)
	result.RequestSnippet = clip("STEP1:\n"+step1Req+"\n\nSTEP2:\n"+step2Req, snippetLimit)
	tmp := handleChatTextResultAllowRepeatedScalar(result, resp2, "42")
	tmp.ResponseSnippet = clip("STEP1:\n"+step1RespText+"\n\nSTEP2:\n"+step2RespText, snippetLimit)
	updateTraceStepResponse(&tmp, "submit_tool_result", step2RespText)
	return tmp
}

func runChatErrorShape(ctx context.Context, rc RunContext) Result {
	result := baseResult("chat.error_shape", "Chat error shape", rc)
	result.Model = rc.Profile.ChatModel
	headers := requestHeadersForTest(rc.Config, "chat.error_shape")
	payload := baseChatPayload(rc.Profile, rc.Config.Suite)
	payload["messages"] = 1 // invalid messages type
	result.RequestSnippet = clip(prettyJSON(payload), snippetLimit)
	resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Chat, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	result.HTTPStatus = resp.StatusCode
	result.LatencyMS = resp.Latency.Milliseconds()
	result.BytesIn = resp.BytesIn
	result.BytesOut = resp.BytesOut
	result.ResponseSnippet = clip(string(resp.Body), snippetLimit)
	if isEndpointMissing(resp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if resp.StatusCode < 400 {
		return failResult(result, fmt.Errorf("expected error status, got %d", resp.StatusCode), "expected_error")
	}
	if resp.StatusCode >= 500 {
		return failResult(result, fmt.Errorf("expected 4xx client error, got %d", resp.StatusCode), "server_error")
	}
	if strings.TrimSpace(errorMessage(resp.Body)) == "" {
		return failResult(result, errors.New("missing error message"), "schema")
	}
	result.Status = StatusPass
	return result
}

func runChatMemory(ctx context.Context, rc RunContext) Result {
	result := baseResult("chat.memory", "Chat memory", rc)
	result.Model = rc.Profile.ChatModel
	headers := requestHeadersForTest(rc.Config, "chat.memory")
	payload := baseChatPayload(rc.Profile, rc.Config.Suite)
	payload["messages"] = []map[string]string{
		{"role": "user", "content": "Remember: code=999. Reply OK"},
		{"role": "assistant", "content": "OK"},
		{"role": "user", "content": "What is the code? Reply with just the number."},
	}
	result.RequestSnippet = clip(prettyJSON(payload), snippetLimit)
	resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Chat, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	return handleChatTextResult(result, resp, "999")
}

func runChatStructuredSchema(ctx context.Context, rc RunContext) Result {
	result := baseResult("chat.structured.json_schema", "Chat structured json_schema", rc)
	result.Model = rc.Profile.ChatModel
	headers := requestHeadersForTest(rc.Config, "chat.structured.json_schema")
	payload := baseChatPayload(rc.Profile, rc.Config.Suite)
	payload["messages"] = []map[string]string{
		{"role": "system", "content": "Return JSON strictly according to the schema."},
		{"role": "user", "content": "Generate an object with status ok and the number 42."},
	}
	payload["response_format"] = map[string]interface{}{
		"type": "json_schema",
		"json_schema": map[string]interface{}{
			"name": "simple_status",
			"schema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"status": map[string]interface{}{"type": "string"},
					"value":  map[string]interface{}{"type": "integer"},
				},
				"required":             []string{"status", "value"},
				"additionalProperties": false,
			},
			"strict": true,
		},
	}
	result.RequestSnippet = clip(prettyJSON(payload), snippetLimit)
	resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Chat, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	return handleStructuredChat(result, resp, true)
}

func runChatStructuredObject(ctx context.Context, rc RunContext) Result {
	result := baseResult("chat.structured.json_object", "Chat structured json_object", rc)
	result.Model = rc.Profile.ChatModel
	headers := requestHeadersForTest(rc.Config, "chat.structured.json_object")
	payload := baseChatPayload(rc.Profile, rc.Config.Suite)
	payload["messages"] = []map[string]string{
		{"role": "system", "content": "You output JSON only."},
		{"role": "user", "content": "Return JSON like {\"ok\":true}."},
	}
	payload["response_format"] = map[string]interface{}{
		"type": "json_object",
	}
	result.RequestSnippet = clip(prettyJSON(payload), snippetLimit)
	resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Chat, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	return handleStructuredChat(result, resp, false)
}

func baseResult(id, name string, rc RunContext) Result {
	return Result{
		TestID:       id,
		TestName:     name,
		Profile:      rc.Profile.Name,
		Pass:         rc.Pass,
		Model:        rc.Profile.ResponsesModel,
		Status:       StatusFail,
		IsWarmup:     rc.IsWarmup,
		snippetLimit: snippetLimit,
	}
}

func handleResponsesTextResult(result Result, resp *httpclient.Response, expected string) Result {
	return handleResponsesTextResultMatch(result, resp, expected, false)
}

func handleResponsesTextResultAllowRepeatedScalar(result Result, resp *httpclient.Response, expected string) Result {
	return handleResponsesTextResultMatch(result, resp, expected, true)
}

func handleResponsesTextResultMatch(result Result, resp *httpclient.Response, expected string, allowRepeatedScalar bool) Result {
	result.HTTPStatus = resp.StatusCode
	result.LatencyMS = resp.Latency.Milliseconds()
	result.BytesIn = resp.BytesIn
	result.BytesOut = resp.BytesOut
	result.ResponseSnippet = clip(string(resp.Body), snippetLimit)
	if isEndpointMissing(resp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return failHTTPStatusResult(result, resp)
	}
	text := extractResponseText(resp.Body)
	if text == "" {
		return failResult(result, errors.New("missing text"), "schema")
	}
	if expected != "" && !matchesExpectedText(text, expected, allowRepeatedScalar) {
		return failResult(result, fmt.Errorf("expected %s", expected), "assert")
	}
	result.Status = StatusPass
	result.Tokens = extractUsageTokens(resp.Body)
	return result
}

func handleChatTextResult(result Result, resp *httpclient.Response, expected string) Result {
	return handleChatTextResultMatch(result, resp, expected, false)
}

func handleChatTextResultAllowRepeatedScalar(result Result, resp *httpclient.Response, expected string) Result {
	return handleChatTextResultMatch(result, resp, expected, true)
}

func handleChatTextResultMatch(result Result, resp *httpclient.Response, expected string, allowRepeatedScalar bool) Result {
	result.HTTPStatus = resp.StatusCode
	result.LatencyMS = resp.Latency.Milliseconds()
	result.BytesIn = resp.BytesIn
	result.BytesOut = resp.BytesOut
	result.ResponseSnippet = clip(string(resp.Body), snippetLimit)
	if isEndpointMissing(resp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return failHTTPStatusResult(result, resp)
	}
	text := extractChatContent(resp.Body)
	if text == "" {
		return failResult(result, errors.New("missing content"), "schema")
	}
	if expected != "" && !matchesExpectedText(text, expected, allowRepeatedScalar) {
		return failResult(result, fmt.Errorf("expected %s", expected), "assert")
	}
	result.Status = StatusPass
	result.Tokens = extractUsageTokens(resp.Body)
	return result
}

func handleStructuredResponse(result Result, resp *httpclient.Response, strictSchema bool) Result {
	result.HTTPStatus = resp.StatusCode
	result.LatencyMS = resp.Latency.Milliseconds()
	result.BytesIn = resp.BytesIn
	result.BytesOut = resp.BytesOut
	result.ResponseSnippet = clip(string(resp.Body), snippetLimit)
	if isEndpointMissing(resp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if isUnsupportedFeature(resp.Body) {
		return unsupportedResult(result, "unknown_parameter", errorMessage(resp.Body))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return failHTTPStatusResult(result, resp)
	}
	text := extractResponseText(resp.Body)
	if text == "" {
		return failResult(result, errors.New("missing text"), "schema")
	}
	if !isValidStructured(text, strictSchema) {
		return failResult(result, errors.New("invalid structured json"), "schema")
	}
	result.Status = StatusPass
	return result
}

func handleStructuredChat(result Result, resp *httpclient.Response, strictSchema bool) Result {
	result.HTTPStatus = resp.StatusCode
	result.LatencyMS = resp.Latency.Milliseconds()
	result.BytesIn = resp.BytesIn
	result.BytesOut = resp.BytesOut
	result.ResponseSnippet = clip(string(resp.Body), snippetLimit)
	if isEndpointMissing(resp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if isUnsupportedFeature(resp.Body) {
		return unsupportedResult(result, "unknown_parameter", errorMessage(resp.Body))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return failHTTPStatusResult(result, resp)
	}
	text := extractChatContent(resp.Body)
	if text == "" {
		return failResult(result, errors.New("missing content"), "schema")
	}
	if !isValidStructured(text, strictSchema) {
		return failResult(result, errors.New("invalid structured json"), "schema")
	}
	result.Status = StatusPass
	return result
}

func isValidStructured(text string, strictSchema bool) bool {
	var obj interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &obj); err != nil {
		return false
	}
	if !strictSchema {
		_, ok := obj.(map[string]interface{})
		return ok
	}
	typed, ok := obj.(map[string]interface{})
	if !ok {
		return false
	}
	status, ok1 := typed["status"].(string)
	value, ok2 := typed["value"].(float64)
	if !ok1 || !ok2 {
		return false
	}
	if len(typed) != 2 {
		return false
	}
	if status != "ok" {
		return false
	}
	if math.Mod(value, 1) != 0 {
		return false
	}
	return int(value) == 42
}

func unsupportedResult(result Result, errType, msg string) Result {
	result.Status = StatusUnsupported
	result.ErrorType = errType
	result.ErrorMessage = msg
	return result
}

func failResult(result Result, err error, errType string) Result {
	if isTimeoutErr(err) {
		result.Status = StatusTimeout
		result.ErrorType = "timeout"
		result.ErrorMessage = err.Error()
		return result
	}
	result.Status = StatusFail
	result.ErrorType = errType
	result.ErrorMessage = err.Error()
	return result
}

func failHTTPStatusResult(result Result, resp interface{}) Result {
	statusCode, headers, body := httpStatusParts(resp)
	result.Status = StatusFail
	if statusCode == http.StatusTooManyRequests {
		result.ErrorType = "rate_limit"
	} else {
		result.ErrorType = "http_status"
	}
	result.ErrorMessage = httpStatusMessage(statusCode, headers, body)
	return result
}

func httpStatusParts(resp interface{}) (int, http.Header, []byte) {
	switch v := resp.(type) {
	case *httpclient.Response:
		if v == nil {
			return 0, nil, nil
		}
		return v.StatusCode, v.Headers, v.Body
	case *httpclient.StreamResult:
		if v == nil {
			return 0, nil, nil
		}
		return v.StatusCode, v.Headers, v.Body
	default:
		return 0, nil, nil
	}
}

func httpStatusMessage(statusCode int, headers http.Header, body []byte) string {
	if statusCode == 0 {
		return "status unknown"
	}
	parts := make([]string, 0, 2)
	if msg := strings.TrimSpace(extractErrorMessage(body)); msg != "" {
		parts = append(parts, msg)
	}
	if statusCode == http.StatusTooManyRequests {
		if hint := rateLimitHint(headers, body, time.Now()); hint != "" && !containsExact(parts, hint) {
			parts = append(parts, hint)
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("status %d", statusCode)
	}
	return fmt.Sprintf("status %d: %s", statusCode, strings.Join(parts, "; "))
}

func containsExact(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func setSnippetLimit(limit int) {
	if limit < 0 {
		limit = 0
	}
	snippetLimit = limit
}

func withTraceStep(result *Result, name, request, response string) {
	if result == nil {
		return
	}
	step := TraceStep{Name: name}
	if request != "" {
		step.Request = clip(request, result.snippetLimit)
	}
	if response != "" {
		step.Response = clip(response, result.snippetLimit)
	}
	result.TraceSteps = append(result.TraceSteps, step)
}

func updateTraceStepResponse(result *Result, name, response string) {
	if result == nil {
		return
	}
	clipped := clip(response, result.snippetLimit)
	for i := range result.TraceSteps {
		if result.TraceSteps[i].Name == name {
			result.TraceSteps[i].Response = clipped
			return
		}
	}
	result.TraceSteps = append(result.TraceSteps, TraceStep{Name: name, Response: clipped})
}

func EffectiveTraceSteps(result Result) []TraceStep {
	if len(result.TraceSteps) > 0 {
		return result.TraceSteps
	}
	if result.RequestSnippet == "" && result.ResponseSnippet == "" {
		return nil
	}
	return []TraceStep{{
		Name:     "main",
		Request:  result.RequestSnippet,
		Response: result.ResponseSnippet,
	}}
}

func clip(s string, n int) string {
	s = trimLeadingBlankLines(s)
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func trimLeadingBlankLines(s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i == 0 {
		return s
	}
	if i >= len(lines) {
		return ""
	}
	return strings.Join(lines[i:], "\n")
}

func prettyJSON(v interface{}) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

func isEndpointMissing(status int) bool {
	return status == 404 || status == 405
}

func isUnknownParam(body []byte) bool {
	msg := strings.ToLower(errorMessage(body))
	if msg == "" {
		return false
	}
	return strings.Contains(msg, "unknown parameter") ||
		strings.Contains(msg, "unrecognized") ||
		strings.Contains(msg, "unsupported") ||
		strings.Contains(msg, "unknown field") ||
		strings.Contains(msg, "extra fields not permitted") ||
		strings.Contains(msg, "unexpected keyword argument") ||
		strings.Contains(msg, "got an unexpected keyword argument")
}

func isUnsupportedFeature(body []byte) bool {
	msg := strings.ToLower(errorMessage(body))
	if msg == "" {
		return isStoreUnsupported(body)
	}
	if isUnknownParam(body) || isUnexpectedEndpointOrMethod(body) {
		return true
	}
	return strings.Contains(msg, "invalid tool_choice type") ||
		strings.Contains(msg, "supported string values: none, auto, required") ||
		strings.Contains(msg, "expected 'none' | 'auto' | 'required'") ||
		strings.Contains(msg, "'response_format.type' must be 'json_schema' or 'text'") ||
		strings.Contains(msg, "\"response_format.type\" must be \"json_schema\" or \"text\"") ||
		isStoreUnsupported(body)
}

func isStoreUnsupported(body []byte) bool {
	msg := strings.ToLower(string(body))
	return strings.Contains(msg, "expected false") && strings.Contains(msg, "store")
}

func isGetUnsupported(body []byte) bool {
	msg := strings.ToLower(errorMessage(body))
	if msg == "" {
		return false
	}
	if strings.Contains(msg, "get responses") && strings.Contains(msg, "not supported") {
		return true
	}
	if strings.Contains(msg, "get") && strings.Contains(msg, "not supported") {
		return true
	}
	return false
}

func isUnexpectedEndpointOrMethod(body []byte) bool {
	msg := strings.ToLower(errorMessage(body))
	return strings.Contains(msg, "unexpected endpoint or method")
}

func isHTMLDocument(body []byte) bool {
	trimmed := strings.TrimSpace(strings.ToLower(string(body)))
	return strings.HasPrefix(trimmed, "<!doctype html") || strings.HasPrefix(trimmed, "<html")
}

func errorMessage(body []byte) string {
	msg := extractErrorMessage(body)
	if hint := rateLimitHint(nil, body, time.Now()); hint != "" {
		if msg == "" {
			return hint
		}
		if !strings.Contains(strings.ToLower(msg), strings.ToLower(hint)) {
			return msg + "; " + hint
		}
	}
	return msg
}

func extractErrorMessage(body []byte) string {
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return ""
	}
	if msg, ok := doc["error"].(string); ok {
		return msg
	}
	if errVal, ok := doc["error"].(map[string]interface{}); ok {
		if msg, ok := errVal["message"].(string); ok {
			return msg
		}
		if msg, ok := errVal["error"].(string); ok {
			return msg
		}
	}
	if msg, ok := doc["message"].(string); ok {
		return msg
	}
	return ""
}

func rateLimitHint(headers http.Header, body []byte, now time.Time) string {
	if delay, ok := httpclient.RetryDelayFromHeaders(headers, now); ok {
		return formatRetryDelay(delay)
	}
	if raw := extractBodyHeaderValue(body, "Retry-After"); raw != "" {
		if delay, ok := httpclient.RetryDelayFromHeaderValue(raw, now); ok {
			return formatRetryDelay(delay)
		}
	}
	if raw := extractBodyHeaderValue(body, "X-RateLimit-Reset"); raw != "" {
		if delay, ok := httpclient.RetryDelayFromHeaderValue(raw, now); ok {
			return formatRetryDelay(delay)
		}
	}
	return ""
}

func formatRetryDelay(delay time.Duration) string {
	if delay <= 0 {
		return "retry now"
	}
	if delay < time.Second {
		delay = time.Second
	}
	return "retry in ~" + delay.Round(time.Second).String()
}

func extractBodyHeaderValue(body []byte, key string) string {
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return ""
	}
	if raw := lookupNestedHeaderValue(doc, key, "error", "metadata", "headers"); raw != "" {
		return raw
	}
	if raw := lookupNestedHeaderValue(doc, key, "metadata", "headers"); raw != "" {
		return raw
	}
	return ""
}

func lookupNestedHeaderValue(doc map[string]interface{}, key string, path ...string) string {
	var current interface{} = doc
	for _, segment := range path {
		next, ok := current.(map[string]interface{})
		if !ok {
			return ""
		}
		current, ok = next[segment]
		if !ok {
			return ""
		}
	}
	headers, ok := current.(map[string]interface{})
	if !ok {
		return ""
	}
	for k, v := range headers {
		if strings.EqualFold(k, key) {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

func isModelsListLike(body []byte) bool {
	var doc interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return false
	}
	switch v := doc.(type) {
	case []interface{}:
		return true
	case map[string]interface{}:
		if data, ok := v["data"].([]interface{}); ok {
			return len(data) >= 0
		}
	}
	return false
}

func extractResponseID(body []byte) string {
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return ""
	}
	if id, ok := doc["id"].(string); ok {
		return id
	}
	return ""
}

func extractPreviousResponseID(body []byte) string {
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return ""
	}
	if id, ok := doc["previous_response_id"].(string); ok {
		return id
	}
	return ""
}

func extractResponseText(body []byte) string {
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return ""
	}
	if text, ok := doc["output_text"].(string); ok {
		return text
	}
	var parts []string
	if out, ok := doc["output"].([]interface{}); ok {
		for _, item := range out {
			m, _ := item.(map[string]interface{})
			if m == nil {
				continue
			}
			if t, _ := m["type"].(string); t == "output_text" {
				if text, ok := m["text"].(string); ok {
					parts = append(parts, text)
				}
				continue
			}
			if t, _ := m["type"].(string); t == "message" {
				if content, ok := m["content"].([]interface{}); ok {
					for _, c := range content {
						cm, _ := c.(map[string]interface{})
						if cm == nil {
							continue
						}
						if t, _ := cm["type"].(string); t == "output_text" || t == "text" {
							if text, ok := cm["text"].(string); ok {
								parts = append(parts, text)
							}
						}
					}
				}
			}
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "")
	}
	if content, ok := doc["content"]; ok {
		if text := extractTextFromContent(content); text != "" {
			return text
		}
	}
	if msg, ok := doc["message"].(map[string]interface{}); ok {
		if text := extractTextFromContent(msg["content"]); text != "" {
			return text
		}
	}
	if text := extractChatContent(body); text != "" {
		return text
	}
	return ""
}

func extractResponseFunctionCall(body []byte) (string, string) {
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", ""
	}
	out, ok := doc["output"].([]interface{})
	if ok {
		for _, item := range out {
			m, _ := item.(map[string]interface{})
			if m == nil {
				continue
			}
			if t, _ := m["type"].(string); t == "function_call" {
				callID := firstString(m["call_id"], m["id"])
				args := anyToString(m["arguments"])
				return callID, args
			}
		}
	}
	// Fallback: some proxies return a Chat Completions-shaped payload on /responses.
	callID, args := extractChatToolCall(body)
	return callID, args
}

func extractResponseFunctionCallItem(body []byte) map[string]interface{} {
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil
	}
	out, ok := doc["output"].([]interface{})
	if !ok {
		return nil
	}
	for _, item := range out {
		m, _ := item.(map[string]interface{})
		if m == nil {
			continue
		}
		if t, _ := m["type"].(string); t == "function_call" {
			call := map[string]interface{}{
				"type":      "function_call",
				"call_id":   firstString(m["call_id"], m["id"]),
				"name":      firstString(m["name"]),
				"arguments": anyToString(m["arguments"]),
			}
			if id := firstString(m["id"]); id != "" {
				call["id"] = id
			}
			return call
		}
	}
	return nil
}

func buildResponsesToolFollowupInput(body []byte, callID, callArgs, toolName, step1Prompt, step2Prompt, output string) []interface{} {
	call := extractResponseFunctionCallItem(body)
	if call == nil {
		call = map[string]interface{}{
			"type":      "function_call",
			"call_id":   callID,
			"name":      toolName,
			"arguments": callArgs,
		}
	}
	if callID == "" {
		callID = firstString(call["call_id"], call["id"])
	}
	if callArgs == "" {
		callArgs = anyToString(call["arguments"])
	}
	call["call_id"] = callID
	call["name"] = firstString(call["name"], toolName)
	call["arguments"] = callArgs
	if firstString(call["id"]) == "" && callID != "" {
		call["id"] = "fc_" + sanitizeIdentifier(callID)
	}

	outputItem := map[string]interface{}{
		"type":    "function_call_output",
		"call_id": callID,
		"output":  output,
	}
	if callID != "" {
		outputItem["id"] = "fco_" + sanitizeIdentifier(callID)
	}

	input := []interface{}{
		map[string]interface{}{"type": "message", "role": "user", "content": step1Prompt},
		call,
		outputItem,
	}
	if strings.TrimSpace(step2Prompt) != "" {
		input = append(input, map[string]interface{}{"type": "message", "role": "user", "content": step2Prompt})
	}
	return input
}

func sanitizeIdentifier(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "tmp"
	}
	return b.String()
}

func matchesExpectedText(text string, expected string, allowRepeatedScalar bool) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == expected {
		return true
	}
	if !allowRepeatedScalar {
		return false
	}
	fields := strings.Fields(trimmed)
	if len(fields) > 1 {
		for _, field := range fields {
			if field != expected {
				return false
			}
		}
		return true
	}
	return false
}

func validateAddToolCallArgs(raw string, wantA, wantB int) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New("missing tool call arguments")
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return fmt.Errorf("invalid tool call arguments json: %w", err)
	}
	if len(args) != 2 {
		return fmt.Errorf("unexpected tool call arguments: %s", raw)
	}
	a, okA := args["a"].(float64)
	b, okB := args["b"].(float64)
	if !okA || !okB {
		return fmt.Errorf("unexpected tool call arguments: %s", raw)
	}
	if math.Mod(a, 1) != 0 || math.Mod(b, 1) != 0 {
		return fmt.Errorf("unexpected tool call arguments: %s", raw)
	}
	if int(a) != wantA || int(b) != wantB {
		return fmt.Errorf("unexpected tool call arguments: %s", raw)
	}
	return nil
}

func extractChatContent(body []byte) string {
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return ""
	}
	choices, ok := doc["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return ""
	}
	c0, _ := choices[0].(map[string]interface{})
	if c0 == nil {
		return ""
	}
	if msg, ok := c0["message"].(map[string]interface{}); ok {
		if text := extractTextFromContent(msg["content"]); text != "" {
			return text
		}
	}
	if text, ok := c0["text"].(string); ok {
		return text
	}
	return ""
}

func extractTextFromContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		parts := make([]string, 0)
		for _, item := range v {
			m, _ := item.(map[string]interface{})
			if m == nil {
				continue
			}
			if t, _ := m["type"].(string); t == "output_text" || t == "text" {
				if text, ok := m["text"].(string); ok {
					parts = append(parts, text)
					continue
				}
			}
			if text, ok := m["text"].(string); ok {
				parts = append(parts, text)
			}
			if text, ok := m["content"].(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	case map[string]interface{}:
		if text, ok := v["text"].(string); ok {
			return text
		}
		if text, ok := v["content"].(string); ok {
			return text
		}
	}
	return ""
}

func extractChatToolCall(body []byte) (string, string) {
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", ""
	}
	choices, ok := doc["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "", ""
	}
	c0, _ := choices[0].(map[string]interface{})
	if c0 == nil {
		return "", ""
	}
	msg, ok := c0["message"].(map[string]interface{})
	if !ok {
		return "", ""
	}
	callsVal, hasCalls := msg["tool_calls"]
	var calls []interface{}
	if hasCalls {
		switch v := callsVal.(type) {
		case []interface{}:
			calls = v
		case map[string]interface{}:
			calls = []interface{}{v}
		}
	}
	if len(calls) == 0 {
		return "", ""
	}
	call, _ := calls[0].(map[string]interface{})
	if call == nil {
		return "", ""
	}
	id := firstString(call["id"], call["tool_call_id"])
	fn, _ := call["function"].(map[string]interface{})
	args := ""
	if fn != nil {
		args = anyToString(fn["arguments"])
	}
	return id, args
}

func chatToolCallMessage(body []byte) map[string]interface{} {
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return map[string]interface{}{"role": "assistant"}
	}
	choices, ok := doc["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return map[string]interface{}{"role": "assistant"}
	}
	c0, _ := choices[0].(map[string]interface{})
	if c0 == nil {
		return map[string]interface{}{"role": "assistant"}
	}
	msg, ok := c0["message"].(map[string]interface{})
	if !ok {
		return map[string]interface{}{"role": "assistant"}
	}
	return msg
}

func extractUsageTokens(body []byte) int {
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return 0
	}
	usage, ok := doc["usage"].(map[string]interface{})
	if !ok {
		return 0
	}
	if total, ok := usage["total_tokens"].(float64); ok {
		return int(total)
	}
	if total, ok := usage["input_tokens"].(float64); ok {
		return int(total)
	}
	return 0
}

func parseChatStreamEvent(data string) (string, bool) {
	if strings.TrimSpace(data) == "[DONE]" {
		return "", true
	}
	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(data), &doc); err != nil {
		return "", false
	}
	choices, ok := doc["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "", false
	}
	c0, _ := choices[0].(map[string]interface{})
	if c0 == nil {
		return "", false
	}
	if finish, ok := c0["finish_reason"].(string); ok && finish != "" {
		return "", true
	}
	delta, ok := c0["delta"].(map[string]interface{})
	if !ok {
		return "", false
	}
	if content, ok := delta["content"].(string); ok {
		return content, false
	}
	return "", false
}

func parseResponsesStreamEvent(data string) (string, bool) {
	if strings.TrimSpace(data) == "[DONE]" {
		return "", true
	}
	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(data), &doc); err != nil {
		return "", false
	}
	if t, ok := doc["type"].(string); ok {
		if strings.HasSuffix(t, ".done") || strings.HasSuffix(t, ".completed") || strings.Contains(t, "response.completed") {
			return "", true
		}
	}
	if delta, ok := doc["delta"].(map[string]interface{}); ok {
		if text, ok := delta["text"].(string); ok {
			return text, false
		}
	}
	if delta, ok := doc["delta"].(string); ok {
		return delta, false
	}
	if text, ok := doc["text"].(string); ok {
		return text, false
	}
	return "", false
}

// runResponsesStream implemented after helper to avoid forward ref
func runResponsesStream(ctx context.Context, rc RunContext) Result {
	result := baseResult("responses.stream", "Responses streaming", rc)
	headers := cloneHeaders(requestHeadersForTest(rc.Config, "responses.stream"))
	headers["Accept"] = "text/event-stream"
	payload := baseResponsesPayload(rc.Profile)
	payload["stream"] = true
	payload["input"] = "Stream test: print 'HELLO' one char at a time"
	result.RequestSnippet = clip(prettyJSON(payload), snippetLimit)
	var text strings.Builder
	var raw strings.Builder
	done := false
	resp, err := rc.Client.PostJSONStream(ctx, rc.Config.Endpoints.Paths.Responses, payload, headers, func(ev sse.Event) error {
		if ev.Data == "" {
			return nil
		}
		raw.WriteString(ev.Data)
		raw.WriteString("\n")
		delta, isDone := parseResponsesStreamEvent(ev.Data)
		if delta != "" {
			text.WriteString(delta)
		}
		if isDone {
			done = true
		}
		return nil
	})
	if err != nil {
		result.ResponseSnippet = clip("SSE:\n"+raw.String()+"\nTEXT:\n"+text.String(), snippetLimit)
		return failResult(result, err, "http_error")
	}
	result.HTTPStatus = resp.StatusCode
	result.LatencyMS = resp.Latency.Milliseconds()
	result.BytesIn = resp.BytesIn
	result.BytesOut = resp.BytesOut
	result.ResponseSnippet = clip("SSE:\n"+raw.String()+"\nTEXT:\n"+text.String(), snippetLimit)
	if isEndpointMissing(resp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if isUnsupportedFeature(resp.Body) {
		return unsupportedResult(result, "unknown_parameter", errorMessage(resp.Body))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return failHTTPStatusResult(result, resp)
	}
	if !done {
		return failResult(result, errors.New("stream did not terminate"), "stream")
	}
	if strings.TrimSpace(text.String()) == "" {
		return failResult(result, errors.New("empty stream"), "stream")
	}
	result.Status = StatusPass
	return result
}
