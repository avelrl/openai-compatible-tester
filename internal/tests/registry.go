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

	ErrorTypeCapabilityUnsupported = "capability_unsupported"
	ErrorTypeCapabilityDisabled    = "capability_disabled"
	ErrorTypeDependencyUnavailable = "dependency_unavailable"
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
	CompatStatus         Status
	SpecStatus           Status
	HTTPStatus           int
	LatencyMS            int64
	BytesIn              int64
	BytesOut             int64
	Tokens               int
	ErrorType            string
	ErrorMessage         string
	CompatErrorType      string
	CompatErrorMessage   string
	SpecErrorType        string
	SpecErrorMessage     string
	Profile              string
	Pass                 int
	Attempts             int
	Model                string
	RequestSnippet       string
	ResponseSnippet      string
	TraceSteps           []TraceStep
	ToolChoiceMode       string
	EffectiveToolChoice  string
	ToolChoiceFallback   bool
	ReasoningEffort      string
	LiteLLMTimeout       string
	FunctionCallObserved bool
	IsWarmup             bool
	Evidence             *Evidence
	snippetLimit         int
}

type TestCase struct {
	ID       string
	Name     string
	Category string
	Family   string
	Target   string

	RequiresStream        bool
	RequiresTools         bool
	RequiresStructured    bool
	RequiresConversations bool
	RequiresMemory        bool
	RequiredCapabilities  []string
	Kind                  string // responses, chat, sanity

	Run func(ctx context.Context, rc RunContext) Result
}

func IsCapabilityGateErrorType(errorType string) bool {
	switch strings.TrimSpace(errorType) {
	case ErrorTypeCapabilityUnsupported, ErrorTypeCapabilityDisabled, ErrorTypeDependencyUnavailable:
		return true
	default:
		return false
	}
}

func (t TestCase) AppliesToTarget(target string) bool {
	testTarget := strings.TrimSpace(t.Target)
	if testTarget == "" {
		return true
	}
	return testTarget == strings.TrimSpace(target)
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
			ID:            "responses.custom_tool",
			Name:          "Responses custom tool",
			Category:      "responses",
			Kind:          KindResponses,
			RequiresTools: true,
			Run:           runResponsesCustomTool,
		},
		{
			ID:            "responses.custom_tool.grammar",
			Name:          "Responses custom tool (grammar)",
			Category:      "responses",
			Kind:          KindResponses,
			RequiresTools: true,
			Run:           runResponsesCustomToolGrammar,
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
			ID:                   "responses.previous_response.chain",
			Name:                 "Responses previous_response_id chain",
			Category:             "responses",
			Family:               "state",
			Target:               "llama_shim",
			Kind:                 KindResponses,
			RequiresMemory:       true,
			RequiredCapabilities: []string{"responses.store"},
			Run:                  runLlamaShimResponsesPreviousResponseChain,
		},
		{
			ID:                   "responses.retrieve.input_items",
			Name:                 "Responses retrieve input_items",
			Category:             "responses",
			Family:               "state",
			Target:               "llama_shim",
			Kind:                 KindResponses,
			RequiredCapabilities: []string{"responses.input_items"},
			Run:                  runLlamaShimResponsesRetrieveInputItems,
		},
		{
			ID:                    "conversations.create.retrieve",
			Name:                  "Conversations create + GET",
			Category:              "responses",
			Family:                "state",
			Target:                "llama_shim",
			Kind:                  KindResponses,
			RequiresConversations: true,
			Run:                   runLlamaShimConversationsCreateRetrieve,
		},
		{
			ID:                    "conversations.items.list",
			Name:                  "Conversations list items",
			Category:              "responses",
			Family:                "state",
			Target:                "llama_shim",
			Kind:                  KindResponses,
			RequiresConversations: true,
			RequiredCapabilities:  []string{"conversations.items"},
			Run:                   runLlamaShimConversationsItemsList,
		},
		{
			ID:                    "conversations.items.append",
			Name:                  "Conversations append items",
			Category:              "responses",
			Family:                "state",
			Target:                "llama_shim",
			Kind:                  KindResponses,
			RequiresConversations: true,
			RequiredCapabilities:  []string{"conversations.items"},
			Run:                   runLlamaShimConversationsItemsAppend,
		},
		{
			ID:                   "responses.compaction",
			Name:                 "Responses compaction",
			Category:             "responses",
			Family:               "compaction",
			Target:               "llama_shim",
			Kind:                 KindResponses,
			RequiredCapabilities: []string{"responses.compaction"},
			Run:                  runLlamaShimResponsesCompaction,
		},
		{
			ID:                   "responses.compaction.auto",
			Name:                 "Responses auto compaction",
			Category:             "responses",
			Family:               "compaction",
			Target:               "llama_shim",
			Kind:                 KindResponses,
			RequiredCapabilities: []string{"responses.compaction.auto", "responses.store"},
			Run:                  runLlamaShimResponsesAutoCompaction,
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

func normalizeInstructionRole(role string) string {
	return strings.ToLower(strings.TrimSpace(role))
}

func effectiveInstructionRole(cfg config.Config, profile config.ModelProfile, testID, defaultRole string) string {
	if ov, ok := testOverrideForProfile(cfg, profile, testID); ok {
		if role := normalizeInstructionRole(ov.InstructionRole); role != "" {
			return role
		}
	}
	return normalizeInstructionRole(defaultRole)
}

func shouldMergeInstructionIntoUser(cfg config.Config, profile config.ModelProfile, testID string) bool {
	if ov, ok := testOverrideForProfile(cfg, profile, testID); ok && ov.MergeInstructionIntoUser != nil {
		return *ov.MergeInstructionIntoUser
	}
	return false
}

func mergeInstructionAndUser(instruction, user string) string {
	instruction = strings.TrimSpace(instruction)
	user = strings.TrimSpace(user)
	switch {
	case instruction == "":
		return user
	case user == "":
		return instruction
	default:
		return instruction + "\n\n" + user
	}
}

func chatInstructionMessage(cfg config.Config, profile config.ModelProfile, testID, defaultRole, content string) map[string]string {
	return map[string]string{
		"role":    effectiveInstructionRole(cfg, profile, testID, defaultRole),
		"content": content,
	}
}

func chatMessagesWithInstruction(cfg config.Config, profile config.ModelProfile, testID, defaultRole, instruction, user string) []map[string]string {
	if shouldMergeInstructionIntoUser(cfg, profile, testID) {
		return []map[string]string{{
			"role":    "user",
			"content": mergeInstructionAndUser(instruction, user),
		}}
	}
	return []map[string]string{
		chatInstructionMessage(cfg, profile, testID, defaultRole, instruction),
		{"role": "user", "content": user},
	}
}

func responsesInputWithOptionalInstruction(cfg config.Config, profile config.ModelProfile, testID, defaultRole, instruction, user, plainFallback string) interface{} {
	if shouldMergeInstructionIntoUser(cfg, profile, testID) {
		return []map[string]string{{
			"role":    "user",
			"content": mergeInstructionAndUser(instruction, user),
		}}
	}
	role := effectiveInstructionRole(cfg, profile, testID, defaultRole)
	if role == "" {
		return plainFallback
	}
	input := []map[string]string{{
		"role":    role,
		"content": instruction,
	}}
	if strings.TrimSpace(user) != "" {
		input = append(input, map[string]string{
			"role":    "user",
			"content": user,
		})
	}
	return input
}

func responsesInstructionInput(cfg config.Config, profile config.ModelProfile, testID, defaultRole, instruction, user string) []map[string]string {
	if shouldMergeInstructionIntoUser(cfg, profile, testID) {
		return []map[string]string{{
			"role":    "user",
			"content": mergeInstructionAndUser(instruction, user),
		}}
	}
	role := effectiveInstructionRole(cfg, profile, testID, defaultRole)
	return []map[string]string{
		{"role": role, "content": instruction},
		{"role": "user", "content": user},
	}
}

func responsesConversationInstructionItem(cfg config.Config, profile config.ModelProfile, testID, defaultRole, content string) map[string]string {
	return map[string]string{
		"type":    "message",
		"role":    effectiveInstructionRole(cfg, profile, testID, defaultRole),
		"content": content,
	}
}

func overridePromptText(raw, fallback string) string {
	if text := strings.TrimSpace(raw); text != "" {
		return text
	}
	return fallback
}

func applyChatReasoningOverride(payload map[string]interface{}, effort string) {
	effort = strings.TrimSpace(effort)
	switch effort {
	case "", "omit":
		delete(payload, "reasoning")
	default:
		payload["reasoning"] = map[string]interface{}{"effort": effort}
	}
}

func applyResponsesReasoningOverride(payload map[string]interface{}, effort string) {
	effort = strings.TrimSpace(effort)
	switch effort {
	case "", "omit":
		delete(payload, "reasoning")
	default:
		payload["reasoning"] = map[string]interface{}{"effort": effort}
	}
}

func effectiveChatMaxTokens(ov config.TestOverride) int {
	if ov.MaxTokens != nil && *ov.MaxTokens > 0 {
		return *ov.MaxTokens
	}
	if ov.MaxOutputTokens != nil && *ov.MaxOutputTokens > 0 {
		// Convenience: allow using max_output_tokens for chat tests too.
		return *ov.MaxOutputTokens
	}
	return 64
}

func effectiveResponsesMaxOutputTokens(ov config.TestOverride) *int {
	if ov.MaxOutputTokens != nil && *ov.MaxOutputTokens > 0 {
		mt := *ov.MaxOutputTokens
		return &mt
	}
	return nil
}

func chatMaxTokensOverride(ov config.TestOverride) *int {
	if ov.MaxTokens != nil && *ov.MaxTokens > 0 {
		mt := *ov.MaxTokens
		return &mt
	}
	if ov.MaxOutputTokens != nil && *ov.MaxOutputTokens > 0 {
		mt := *ov.MaxOutputTokens
		return &mt
	}
	return nil
}

func chatMaxTokensParam(profile config.ModelProfile) string {
	if param := strings.TrimSpace(profile.ChatMaxTokensParam); param != "" {
		return param
	}
	return "max_tokens"
}

func applyChatMaxTokens(payload map[string]interface{}, profile config.ModelProfile, value int) {
	if payload == nil || value <= 0 {
		return
	}
	payload[chatMaxTokensParam(profile)] = value
}

func effectiveChatReasoningEffort(profile config.ModelProfile, suite config.SuiteConfig, ov config.TestOverride) string {
	if eff := strings.TrimSpace(ov.ReasoningEffort); eff != "" {
		return eff
	}
	if suite.ChatReasoning.Enabled {
		if eff := strings.TrimSpace(profile.ReasoningEffort); eff != "" {
			return eff
		}
	}
	return "omit"
}

func effectiveResponsesReasoningEffort(profile config.ModelProfile, ov config.TestOverride) string {
	if eff := strings.TrimSpace(ov.ReasoningEffort); eff != "" {
		return eff
	}
	if eff := strings.TrimSpace(profile.ReasoningEffort); eff != "" {
		return eff
	}
	return "omit"
}

func applyStreamTimeoutHeader(headers map[string]string, cfg config.Config, profile config.ModelProfile, testID string, ov config.TestOverride) {
	if headers == nil {
		return
	}
	if effectiveHeaderValueForTest(cfg, profile, testID, "x-litellm-stream-timeout") != "" {
		return
	}
	if ov.StreamTimeoutSeconds > 0 {
		headers["x-litellm-stream-timeout"] = strconv.Itoa(ov.StreamTimeoutSeconds)
	}
}

func runModelsList(ctx context.Context, rc RunContext) Result {
	result := baseResult("sanity.models", "GET /v1/models", rc)
	headers := requestHeadersForTest(rc.Config, rc.Profile, "sanity.models")
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
	headers := requestHeadersForTest(rc.Config, rc.Profile, "responses.basic")
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, "responses.basic")
	payload := baseResponsesPayload(rc.Profile)
	applyResponsesReasoningOverride(payload, effectiveResponsesReasoningEffort(rc.Profile, ov))
	payload["input"] = responsesInputWithOptionalInstruction(rc.Config, rc.Profile, "responses.basic", "", "Reply with exactly OK", "ping", "ping: answer with OK")
	result.RequestSnippet = clip(prettyJSON(payload), snippetLimit)
	resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	return handleResponsesTextResult(result, resp, "OK")
}

func runResponsesStoreGet(ctx context.Context, rc RunContext) Result {
	result := baseResult("responses.store_get", "Responses store + GET", rc)
	headers := requestHeadersForTest(rc.Config, rc.Profile, "responses.store_get")
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, "responses.store_get")
	payload := baseResponsesPayload(rc.Profile)
	applyResponsesReasoningOverride(payload, effectiveResponsesReasoningEffort(rc.Profile, ov))
	payload["input"] = responsesInputWithOptionalInstruction(rc.Config, rc.Profile, "responses.store_get", "", "Say OK and nothing else", "", "Say OK and nothing else")
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
		return unsupportedFeatureResult(result, resp.Body)
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
		if isEndpointMissing(getResp.StatusCode) || wrappedUpstreamStatus(getResp.Body, getResp.StatusCode) == http.StatusNotFound {
			return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", getResp.StatusCode))
		}
		if isGetUnsupported(getResp.Body) {
			return unsupportedResult(result, "unsupported_get", errorMessage(getResp.Body))
		}
		return failHTTPStatusResult(result, getResp)
	}
	result.HTTPStatus = getResp.StatusCode
	result.LatencyMS = getResp.Latency.Milliseconds()
	result.BytesIn = getResp.BytesIn
	result.BytesOut = getResp.BytesOut
	recordResponsesBodyEvidence(&result, getResp.Body)
	if strings.TrimSpace(extractResponseText(getResp.Body)) == "" {
		return failResult(result, errors.New("missing text"), "schema")
	}
	if gotID := extractResponseID(getResp.Body); gotID != "" && gotID != id {
		return failResult(result, fmt.Errorf("id mismatch: %s != %s", gotID, id), "schema")
	}
	result.Status = StatusPass
	result.Tokens = extractUsageTokens(getResp.Body)
	return result
}

func runResponsesStructuredSchema(ctx context.Context, rc RunContext) Result {
	result := baseResult("responses.structured.json_schema", "Responses structured json_schema", rc)
	headers := requestHeadersForTest(rc.Config, rc.Profile, "responses.structured.json_schema")
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, "responses.structured.json_schema")
	payload := baseResponsesPayload(rc.Profile)
	applyResponsesReasoningOverride(payload, effectiveResponsesReasoningEffort(rc.Profile, ov))
	if maxOutputTokens := effectiveResponsesMaxOutputTokens(ov); maxOutputTokens != nil {
		payload["max_output_tokens"] = *maxOutputTokens
	}
	instruction := overridePromptText(ov.InstructionText, "Return JSON strictly according to the schema.")
	userPrompt := overridePromptText(ov.UserText, "Generate an object with status=\"ok\" and value=42.")
	payload["input"] = responsesInstructionInput(
		rc.Config,
		rc.Profile,
		"responses.structured.json_schema",
		"system",
		instruction,
		userPrompt,
	)
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
	headers := requestHeadersForTest(rc.Config, rc.Profile, "responses.structured.json_object")
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, "responses.structured.json_object")
	payload := baseResponsesPayload(rc.Profile)
	applyResponsesReasoningOverride(payload, effectiveResponsesReasoningEffort(rc.Profile, ov))
	if maxOutputTokens := effectiveResponsesMaxOutputTokens(ov); maxOutputTokens != nil {
		payload["max_output_tokens"] = *maxOutputTokens
	}
	instruction := overridePromptText(ov.InstructionText, "You output JSON only.")
	userPrompt := overridePromptText(ov.UserText, "Return JSON like {\"ok\":true}.")
	payload["input"] = responsesInstructionInput(
		rc.Config,
		rc.Profile,
		"responses.structured.json_object",
		"system",
		instruction,
		userPrompt,
	)
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

func runResponsesCustomTool(ctx context.Context, rc RunContext) Result {
	return runResponsesCustomToolVariant(
		ctx,
		rc,
		"responses.custom_tool",
		"Responses custom tool",
		responsesAddCustomTool("code_exec", "Executes arbitrary Python code"),
		"Use the code_exec tool to print hello world to the console. Do not answer directly.",
		"code_exec",
		validateCustomToolFreeformInput,
	)
}

func runResponsesCustomToolGrammar(ctx context.Context, rc RunContext) Result {
	return runResponsesCustomToolVariant(
		ctx,
		rc,
		"responses.custom_tool.grammar",
		"Responses custom tool (grammar)",
		responsesAddCustomToolWithGrammar("math_exp", "Creates valid mathematical expressions", "lark", responsesMathExpressionGrammar),
		"Use the math_exp tool to add four plus four. Do not answer directly.",
		"math_exp",
		func(input string) error {
			return validateMathExpressionToolInput(input, 8)
		},
	)
}

func runResponsesCustomToolVariant(ctx context.Context, rc RunContext, testID, name string, tool map[string]interface{}, prompt, expectedTool string, validateInput func(string) error) Result {
	result := baseResult(testID, name, rc)
	headers := requestHeadersForTest(rc.Config, rc.Profile, testID)
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, testID)
	reasoningEffort := effectiveResponsesReasoningEffort(rc.Profile, ov)

	payload := baseResponsesPayload(rc.Profile)
	applyResponsesReasoningOverride(payload, reasoningEffort)
	if maxOutputTokens := effectiveResponsesMaxOutputTokens(ov); maxOutputTokens != nil {
		payload["max_output_tokens"] = *maxOutputTokens
	}
	result.ToolChoiceMode = "required"
	result.ReasoningEffort = reasoningEffort
	payload["tool_choice"] = "required"
	payload["tools"] = []map[string]interface{}{tool}
	payload["input"] = prompt

	req := prettyJSON(payload)
	result.RequestSnippet = clip(req, snippetLimit)
	withTraceStep(&result, "request_custom_tool", req, "")

	resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	result.HTTPStatus = resp.StatusCode
	result.LatencyMS = resp.Latency.Milliseconds()
	result.BytesIn = resp.BytesIn
	result.BytesOut = resp.BytesOut
	result.ResponseSnippet = clip(string(resp.Body), snippetLimit)
	updateTraceStepResponse(&result, "request_custom_tool", string(resp.Body))
	recordResponsesBodyEvidence(&result, resp.Body)

	if isEndpointMissing(resp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if isUnsupportedFeature(resp.Body) {
		return unsupportedFeatureResult(result, resp.Body)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return failHTTPStatusResult(result, resp)
	}

	_, actualTool, input := extractResponseCustomToolCall(resp.Body)
	if strings.TrimSpace(actualTool) == "" {
		return failResult(result, errors.New("missing custom tool call"), "tool_call")
	}
	ensureEvidence(&result).CanonicalCustomToolCallSeen = true
	if actualTool != expectedTool {
		return failResult(result, fmt.Errorf("unexpected custom tool name: %s", actualTool), "tool_call")
	}
	if err := validateInput(input); err != nil {
		return failResult(result, err, "tool_call")
	}

	result.Status = StatusPass
	result.Tokens = extractUsageTokens(resp.Body)
	return result
}

func runResponsesToolCallVariant(ctx context.Context, rc RunContext, testID, name, forcedMode string) Result {
	result := baseResult(testID, name, rc)
	headers := requestHeadersForTest(rc.Config, rc.Profile, testID)
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, testID)

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
	reasoningEffort := effectiveResponsesReasoningEffort(rc.Profile, ov)
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
	maxOutputTokens := effectiveResponsesMaxOutputTokens(ov)
	useStream := false
	if rc.Config.Suite.Stream.Enabled && ov.Stream != nil {
		useStream = *ov.Stream
	}

	result.ToolChoiceMode = mode
	result.ReasoningEffort = reasoningEffort
	result.LiteLLMTimeout = effectiveHeaderValueForTest(rc.Config, rc.Profile, testID, "x-litellm-timeout")

	payload := baseResponsesPayload(rc.Profile)
	switch mode {
	case "forced":
		payload["tool_choice"] = map[string]interface{}{"type": "function", "name": toolName}
	case "forced_compat":
		// Some OpenAI-compatible shims reject the object form of tool_choice
		// but still honor "required" when exactly one tool is exposed.
		payload["tool_choice"] = "required"
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
	payload["tools"] = []map[string]interface{}{responsesAddTool(toolName, strict)}
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
		if effectiveHeaderValueForTest(rc.Config, rc.Profile, testID, "x-litellm-stream-timeout") == "" {
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

			id, _, argsPiece, eventType, canonical := parseResponsesToolCallStreamEventDetailed(ev.Data)
			appendStreamEventType(&result, eventType)
			if id != "" {
				callID = id
			}
			if argsPiece != "" {
				argsBuf = mergeArgsJSON(argsBuf, argsPiece)
				if canonical {
					ensureEvidence(&result).CanonicalStreamToolCallSeen = true
					ensureEvidence(&result).CanonicalToolCallSeen = true
				} else {
					ensureEvidence(&result).FallbackChatToolCallOnResp = true
				}
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
		recordResponsesBodyEvidence(&result, resp.Body)

		if isEndpointMissing(resp.StatusCode) {
			return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
		}
		if resp.StatusCode >= 400 && isUnsupportedFeature(resp.Body) {
			return unsupportedFeatureResult(result, resp.Body)
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
		recordResponsesBodyEvidence(&result, resp.Body)

		if isEndpointMissing(resp.StatusCode) {
			return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
		}
		if isUnsupportedFeature(resp.Body) {
			return unsupportedFeatureResult(result, resp.Body)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return failHTTPStatusResult(result, resp)
		}
		canonicalCallID, _ := extractCanonicalResponseFunctionCall(resp.Body)
		callID, callArgs = extractResponseFunctionCall(resp.Body)
		if callID != "" && canonicalCallID == "" {
			ensureEvidence(&result).FallbackChatToolCallOnResp = true
		}
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
	headers := requestHeadersForTest(rc.Config, rc.Profile, "responses.error_shape")
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
	recordResponsesBodyEvidence(&result, resp.Body)
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
	headers := requestHeadersForTest(rc.Config, rc.Profile, "responses.memory.prev_id")
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, "responses.memory.prev_id")
	payload := baseResponsesPayload(rc.Profile)
	applyResponsesReasoningOverride(payload, effectiveResponsesReasoningEffort(rc.Profile, ov))
	payload["input"] = responsesInputWithOptionalInstruction(rc.Config, rc.Profile, "responses.memory.prev_id", "", "Remember: my code = 123. Reply OK", "", "Remember: my code = 123. Reply OK")
	result.RequestSnippet = clip(prettyJSON(payload), snippetLimit)
	withTraceStep(&result, "remember_value", prettyJSON(payload), "")
	resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	result.HTTPStatus = resp.StatusCode
	result.LatencyMS = resp.Latency.Milliseconds()
	result.BytesIn = resp.BytesIn
	result.BytesOut = resp.BytesOut
	result.ResponseSnippet = clip(string(resp.Body), snippetLimit)
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
	applyResponsesReasoningOverride(payload2, effectiveResponsesReasoningEffort(rc.Profile, ov))
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
	headers := requestHeadersForTest(rc.Config, rc.Profile, "responses.conversations")
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, "responses.conversations")
	payload := map[string]interface{}{
		"items": []map[string]string{
			responsesConversationInstructionItem(rc.Config, rc.Profile, "responses.conversations", "system", "You are a test assistant."),
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
	applyResponsesReasoningOverride(payload2, effectiveResponsesReasoningEffort(rc.Profile, ov))
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
	headers := requestHeadersForTest(rc.Config, rc.Profile, "chat.basic")
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, "chat.basic")
	payload := baseChatPayload(rc.Profile, rc.Config.Suite)
	applyChatReasoningOverride(payload, effectiveChatReasoningEffort(rc.Profile, rc.Config.Suite, ov))
	instruction := overridePromptText(ov.InstructionText, "Reply with exactly OK")
	userPrompt := overridePromptText(ov.UserText, "ping")
	payload["messages"] = chatMessagesWithInstruction(rc.Config, rc.Profile, "chat.basic", "developer", instruction, userPrompt)
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
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, "chat.stream")
	headers := cloneHeaders(requestHeadersForTest(rc.Config, rc.Profile, "chat.stream"))
	headers["Accept"] = "text/event-stream"
	applyStreamTimeoutHeader(headers, rc.Config, rc.Profile, "chat.stream", ov)
	payload := baseChatPayload(rc.Profile, rc.Config.Suite)
	result.ReasoningEffort = effectiveChatReasoningEffort(rc.Profile, rc.Config.Suite, ov)
	applyChatReasoningOverride(payload, result.ReasoningEffort)
	if maxTokens := chatMaxTokensOverride(ov); maxTokens != nil {
		applyChatMaxTokens(payload, rc.Profile, *maxTokens)
	}
	payload["stream"] = true
	instruction := overridePromptText(ov.InstructionText, "Reply with exactly HELLO and nothing else. Stream it one character at a time.")
	userPrompt := overridePromptText(ov.UserText, "go")
	payload["messages"] = chatMessagesWithInstruction(rc.Config, rc.Profile, "chat.stream", "developer", instruction, userPrompt)
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
		delta, isDone, terminal, eventType := parseChatStreamEventDetailed(ev.Data)
		appendStreamEventType(&result, eventType)
		if delta != "" {
			text.WriteString(delta)
			ensureEvidence(&result).CanonicalStreamTextSeen = true
		}
		if isDone {
			if terminal {
				ensureEvidence(&result).CanonicalStreamTerminalSeen = true
			}
			done = true
			return sse.ErrStop
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
	recordChatBodyEvidence(&result, resp.Body)
	if isEndpointMissing(resp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if isUnsupportedFeature(resp.Body) {
		return unsupportedFeatureResult(result, resp.Body)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return failHTTPStatusResult(result, resp)
	}
	if !done && (resp == nil || !resp.Done) {
		return failResult(result, errors.New("stream did not terminate"), "stream")
	}
	if strings.TrimSpace(text.String()) == "" {
		return failResult(result, errors.New("empty stream"), "stream")
	}
	if normalizeStreamText(text.String()) != "HELLO" {
		return failResult(result, errors.New("expected HELLO"), "assert")
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
	headers := requestHeadersForTest(rc.Config, rc.Profile, testID)
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, testID)

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
	maxTokens := effectiveChatMaxTokens(ov)

	result.ToolChoiceMode = mode
	result.ReasoningEffort = effectiveChatReasoningEffort(rc.Profile, rc.Config.Suite, ov)
	result.LiteLLMTimeout = effectiveHeaderValueForTest(rc.Config, rc.Profile, testID, "x-litellm-timeout")

	step1Prompt := fmt.Sprintf("Call %s with a=40 and b=2. Do not answer yourself.", toolName)
	step2Prompt := "Reply with just the number."

	payload := baseChatPayload(rc.Profile, rc.Config.Suite)
	applyChatReasoningOverride(payload, result.ReasoningEffort)
	switch mode {
	case "forced":
		payload["tool_choice"] = map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name": toolName,
			},
		}
	case "forced_compat":
		// Some OpenAI-compatible shims reject the object form of tool_choice
		// but still honor "required" when exactly one tool is exposed.
		payload["tool_choice"] = "required"
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
	applyChatMaxTokens(payload, rc.Profile, maxTokens)
	payload["tools"] = []map[string]interface{}{chatAddTool(toolName, strict)}
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
	recordChatBodyEvidence(&result, resp.Body)
	if isEndpointMissing(resp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if isUnsupportedFeature(resp.Body) {
		return unsupportedFeatureResult(result, resp.Body)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return failHTTPStatusResult(result, resp)
	}
	callID, callArgs := extractChatToolCall(resp.Body)
	if callID == "" {
		return failResult(result, errors.New("missing tool call"), "tool_call")
	}
	ensureEvidence(&result).CanonicalToolCallSeen = true
	if err := validateAddToolCallArgs(callArgs, 40, 2); err != nil {
		return failResult(result, err, "tool_call")
	}
	result.FunctionCallObserved = true

	payload2 := baseChatPayload(rc.Profile, rc.Config.Suite)
	applyChatReasoningOverride(payload2, result.ReasoningEffort)
	payload2["parallel_tool_calls"] = parallel
	applyChatMaxTokens(payload2, rc.Profile, maxTokens)
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
	headers := requestHeadersForTest(rc.Config, rc.Profile, "chat.error_shape")
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, "chat.error_shape")
	payload := baseChatPayload(rc.Profile, rc.Config.Suite)
	applyChatReasoningOverride(payload, effectiveChatReasoningEffort(rc.Profile, rc.Config.Suite, ov))
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
	recordChatBodyEvidence(&result, resp.Body)
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
	headers := requestHeadersForTest(rc.Config, rc.Profile, "chat.memory")
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, "chat.memory")
	payload := baseChatPayload(rc.Profile, rc.Config.Suite)
	applyChatReasoningOverride(payload, effectiveChatReasoningEffort(rc.Profile, rc.Config.Suite, ov))
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
	headers := requestHeadersForTest(rc.Config, rc.Profile, "chat.structured.json_schema")
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, "chat.structured.json_schema")
	payload := baseChatPayload(rc.Profile, rc.Config.Suite)
	applyChatReasoningOverride(payload, effectiveChatReasoningEffort(rc.Profile, rc.Config.Suite, ov))
	if maxTokens := chatMaxTokensOverride(ov); maxTokens != nil {
		applyChatMaxTokens(payload, rc.Profile, *maxTokens)
	}
	instruction := overridePromptText(ov.InstructionText, "Return JSON strictly according to the schema.")
	userPrompt := overridePromptText(ov.UserText, "Generate an object with status=\"ok\" and value=42.")
	payload["messages"] = chatMessagesWithInstruction(rc.Config, rc.Profile, "chat.structured.json_schema", "system", instruction, userPrompt)
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
	headers := requestHeadersForTest(rc.Config, rc.Profile, "chat.structured.json_object")
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, "chat.structured.json_object")
	payload := baseChatPayload(rc.Profile, rc.Config.Suite)
	applyChatReasoningOverride(payload, effectiveChatReasoningEffort(rc.Profile, rc.Config.Suite, ov))
	if maxTokens := chatMaxTokensOverride(ov); maxTokens != nil {
		applyChatMaxTokens(payload, rc.Profile, *maxTokens)
	}
	instruction := overridePromptText(ov.InstructionText, "You output JSON only.")
	userPrompt := overridePromptText(ov.UserText, "Return JSON like {\"ok\":true}.")
	payload["messages"] = chatMessagesWithInstruction(rc.Config, rc.Profile, "chat.structured.json_object", "system", instruction, userPrompt)
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
	recordResponsesBodyEvidence(&result, resp.Body)
	if isEndpointMissing(resp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return failHTTPStatusResult(result, resp)
	}
	canonicalText := extractCanonicalResponseText(resp.Body)
	text := extractResponseText(resp.Body)
	if canonicalText == "" && text != "" {
		ensureEvidence(&result).FallbackChatShapeOnResponses = true
	}
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
	recordChatBodyEvidence(&result, resp.Body)
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
	recordResponsesBodyEvidence(&result, resp.Body)
	if isEndpointMissing(resp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if isUnsupportedFeature(resp.Body) {
		return unsupportedFeatureResult(result, resp.Body)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return failHTTPStatusResult(result, resp)
	}
	canonicalText := extractCanonicalResponseText(resp.Body)
	text := extractResponseText(resp.Body)
	if canonicalText == "" && text != "" {
		ensureEvidence(&result).FallbackChatShapeOnResponses = true
	}
	if text == "" {
		return failResult(result, errors.New("missing text"), "schema")
	}
	if !isValidStructured(text, strictSchema) {
		return failResult(result, errors.New("invalid structured json"), "schema")
	}
	if strings.TrimSpace(canonicalText) != "" && isValidStructured(canonicalText, strictSchema) {
		ensureEvidence(&result).CanonicalStructuredSeen = true
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
	recordChatBodyEvidence(&result, resp.Body)
	if isEndpointMissing(resp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if isUnsupportedFeature(resp.Body) {
		return unsupportedFeatureResult(result, resp.Body)
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
	ensureEvidence(&result).CanonicalStructuredSeen = true
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
	if errType == "endpoint_missing" || errType == "unsupported_get" {
		ev := ensureEvidence(&result)
		ev.StrictUnsupported = true
		ev.StrictUnsupportedReason = strings.TrimSpace(msg)
	}
	return result
}

func unsupportedFeatureResult(result Result, body []byte) Result {
	recordErrorEvidence(&result, body)
	ev := ensureEvidence(&result)
	if result.HTTPStatus >= 400 && result.HTTPStatus < 500 && hasCanonicalErrorObject(ev) && isStrictUnsupportedFeature(body) {
		ev.StrictUnsupported = true
		ev.StrictUnsupportedReason = strings.TrimSpace(errorMessage(body))
	}
	return unsupportedResult(result, "unknown_parameter", errorMessage(body))
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
	appendHTTPStatusDiagnostics(&result, headers)
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

func appendHTTPStatusDiagnostics(result *Result, headers http.Header) {
	if result == nil {
		return
	}
	lines := relevantResponseHeaderLines(headers)
	if len(lines) == 0 {
		return
	}
	diag := "HEADERS:\n" + strings.Join(lines, "\n")
	if len(result.TraceSteps) == 0 {
		withTraceStep(result, "http_error", "", diag)
		return
	}
	last := len(result.TraceSteps) - 1
	if strings.TrimSpace(result.TraceSteps[last].Response) == "" {
		result.TraceSteps[last].Response = clip(diag, result.snippetLimit)
		return
	}
	result.TraceSteps[last].Response = clip(result.TraceSteps[last].Response+"\n\n"+diag, result.snippetLimit)
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

func isStrictUnsupportedFeature(body []byte) bool {
	msg := strings.ToLower(errorMessage(body))
	if msg == "" {
		return isStoreUnsupported(body)
	}
	return strings.Contains(msg, "unknown parameter") ||
		strings.Contains(msg, "unsupported parameter") ||
		strings.Contains(msg, "unsupported value") ||
		strings.Contains(msg, "grammar-constrained custom tools are not supported in bridge mode") ||
		strings.Contains(msg, "unknown field") ||
		strings.Contains(msg, "extra fields not permitted") ||
		strings.Contains(msg, "unexpected keyword argument") ||
		strings.Contains(msg, "got an unexpected keyword argument") ||
		strings.Contains(msg, "invalid tool_choice type") ||
		strings.Contains(msg, "supported string values: none, auto, required") ||
		strings.Contains(msg, "expected 'none' | 'auto' | 'required'") ||
		strings.Contains(msg, "of tool must be 'function'") ||
		strings.Contains(msg, "of tool must be \"function\"") ||
		strings.Contains(msg, "'response_format.type' must be 'json_schema' or 'text'") ||
		strings.Contains(msg, "\"response_format.type\" must be \"json_schema\" or \"text\"") ||
		strings.Contains(msg, "input should be 'text' or 'json_object'") ||
		isStoreUnsupported(body)
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
		strings.Contains(msg, "grammar-constrained custom tools are not supported in bridge mode") ||
		strings.Contains(msg, "supported string values: none, auto, required") ||
		strings.Contains(msg, "expected 'none' | 'auto' | 'required'") ||
		strings.Contains(msg, "of tool must be 'function'") ||
		strings.Contains(msg, "of tool must be \"function\"") ||
		strings.Contains(msg, "'response_format.type' must be 'json_schema' or 'text'") ||
		strings.Contains(msg, "\"response_format.type\" must be \"json_schema\" or \"text\"") ||
		strings.Contains(msg, "input should be 'text' or 'json_object'") ||
		isStoreUnsupported(body)
}

func responsesAddTool(toolName string, strict bool) map[string]interface{} {
	tool := map[string]interface{}{
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
	}
	if strict {
		tool["strict"] = true
	}
	return tool
}

func responsesAddCustomTool(toolName, description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "custom",
		"name":        toolName,
		"description": description,
	}
}

func responsesAddCustomToolWithGrammar(toolName, description, syntax, definition string) map[string]interface{} {
	tool := responsesAddCustomTool(toolName, description)
	tool["format"] = map[string]interface{}{
		"type":       "grammar",
		"syntax":     syntax,
		"definition": definition,
	}
	return tool
}

const responsesMathExpressionGrammar = `
start: expr
expr: term (SP ADD SP term)* -> add
    | term
term: factor (SP MUL SP factor)* -> mul
    | factor
factor: INT
SP: " "
ADD: "+"
MUL: "*"
%import common.INT
`

func validateCustomToolFreeformInput(input string) error {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return errors.New("missing custom tool input")
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return fmt.Errorf("custom tool input must be plain text, got %q", trimmed)
	}
	lower := strings.ToLower(trimmed)
	if !strings.Contains(lower, "print") || !strings.Contains(lower, "hello world") {
		return fmt.Errorf("unexpected custom tool input: %q", trimmed)
	}
	return nil
}

func validateMathExpressionToolInput(input string, want int) error {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return errors.New("missing custom tool input")
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return fmt.Errorf("custom tool input must be plain text, got %q", trimmed)
	}
	got, err := evalSimpleMathExpression(trimmed)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("unexpected math expression result: got %d want %d", got, want)
	}
	return nil
}

func evalSimpleMathExpression(input string) (int, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return 0, errors.New("empty math expression")
	}
	if looksLikeCompactMathExpression(trimmed) {
		return 0, fmt.Errorf("math expression must use single spaces between tokens: %q", trimmed)
	}
	tokens := strings.Fields(trimmed)
	if len(tokens)%2 == 0 {
		return 0, fmt.Errorf("invalid math expression syntax: %q", trimmed)
	}
	normalized := strings.Join(tokens, " ")
	if normalized != trimmed {
		return 0, fmt.Errorf("math expression must use single spaces between tokens: %q", trimmed)
	}

	current, err := strconv.Atoi(tokens[0])
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q", tokens[0])
	}
	sum := 0
	for i := 1; i < len(tokens); i += 2 {
		op := tokens[i]
		next, err := strconv.Atoi(tokens[i+1])
		if err != nil {
			return 0, fmt.Errorf("invalid integer %q", tokens[i+1])
		}
		switch op {
		case "*":
			current *= next
		case "+":
			sum += current
			current = next
		default:
			return 0, fmt.Errorf("unsupported operator %q", op)
		}
	}
	return sum + current, nil
}

func looksLikeCompactMathExpression(input string) bool {
	hasOperator := false
	for _, r := range input {
		switch {
		case r >= '0' && r <= '9':
		case r == '+' || r == '*':
			hasOperator = true
		default:
			return false
		}
	}
	return hasOperator
}

func chatAddTool(toolName string, strict bool) map[string]interface{} {
	fn := map[string]interface{}{
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
	}
	if strict {
		fn["strict"] = true
	}
	return map[string]interface{}{
		"type":     "function",
		"function": fn,
	}
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

func wrappedUpstreamStatus(body []byte, statusCode int) int {
	if statusCode < 500 {
		return 0
	}
	msg := strings.ToLower(errorMessage(body))
	switch {
	case strings.Contains(msg, "'code': 404"), strings.Contains(msg, `"code":404`), strings.Contains(msg, `"code": 404`):
		return http.StatusNotFound
	case strings.Contains(msg, "'code': 405"), strings.Contains(msg, `"code":405`), strings.Contains(msg, `"code": 405`):
		return http.StatusMethodNotAllowed
	case strings.Contains(msg, "'code': 400"), strings.Contains(msg, `"code":400`), strings.Contains(msg, `"code": 400`):
		return http.StatusBadRequest
	default:
		return 0
	}
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
	if raw := extractBodyHeaderValue(body, "RateLimit-Reset"); raw != "" {
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

func relevantResponseHeaderLines(headers http.Header) []string {
	if len(headers) == 0 {
		return nil
	}
	keys := []string{
		"Retry-After",
		"RateLimit-Reset",
		"RateLimit-Limit",
		"RateLimit-Remaining",
		"X-RateLimit-Reset",
		"X-RateLimit-Limit",
		"X-RateLimit-Remaining",
		"X-RateLimit-Reset-Requests",
		"X-RateLimit-Limit-Requests",
		"X-RateLimit-Remaining-Requests",
		"Request-Id",
		"X-Request-Id",
	}
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		if value := strings.TrimSpace(headers.Get(key)); value != "" {
			lines = append(lines, key+": "+value)
		}
	}
	return lines
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
	if text := extractCanonicalResponseText(body); text != "" {
		return text
	}
	return extractChatContent(body)
}

func extractResponseFunctionCall(body []byte) (string, string) {
	if callID, args := extractCanonicalResponseFunctionCall(body); callID != "" {
		return callID, args
	}
	return extractChatToolCall(body)
}

func extractResponseCustomToolCall(body []byte) (string, string, string) {
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", "", ""
	}
	out, ok := doc["output"].([]interface{})
	if !ok {
		return "", "", ""
	}
	for _, item := range out {
		m, _ := item.(map[string]interface{})
		if m == nil {
			continue
		}
		if t, _ := m["type"].(string); t == "custom_tool_call" {
			callID := firstString(m["call_id"], m["id"])
			name := firstString(m["name"])
			input := anyToString(m["input"])
			return callID, name, input
		}
	}
	return "", "", ""
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

func normalizeStreamText(text string) string {
	return strings.Join(strings.Fields(text), "")
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
	delta, done, _, _ := parseChatStreamEventDetailed(data)
	return delta, done
}

func parseChatStreamEventDetailed(data string) (string, bool, bool, string) {
	if strings.TrimSpace(data) == "[DONE]" {
		return "", true, true, "[DONE]"
	}
	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(data), &doc); err != nil {
		return "", false, false, ""
	}
	choices, ok := doc["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "", false, false, ""
	}
	c0, _ := choices[0].(map[string]interface{})
	if c0 == nil {
		return "", false, false, ""
	}
	delta, ok := c0["delta"].(map[string]interface{})
	var content string
	if ok {
		content, _ = delta["content"].(string)
	}
	if finish, ok := c0["finish_reason"].(string); ok && finish != "" {
		return content, true, true, "chat.completions.chunk"
	}
	return content, false, false, "chat.completions.chunk"
}

func parseResponsesStreamEvent(data string) (string, bool) {
	delta, done, _, _ := parseResponsesStreamEventDetailed(data)
	return delta, done
}

func parseResponsesStreamEventDetailed(data string) (string, bool, bool, string) {
	if strings.TrimSpace(data) == "[DONE]" {
		return "", true, false, "[DONE]"
	}
	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(data), &doc); err != nil {
		return "", false, false, ""
	}
	if t, ok := doc["type"].(string); ok {
		switch t {
		case "response.completed":
			return "", true, true, t
		case "response.output_text.delta":
			if delta, ok := doc["delta"].(string); ok {
				return delta, false, true, t
			}
		case "response.output_text.done":
			if text, ok := doc["text"].(string); ok {
				return text, true, true, t
			}
			return "", true, true, t
		case "response.content_part.added":
			part, _ := doc["part"].(map[string]interface{})
			if part == nil {
				return "", false, false, t
			}
			if partType, _ := part["type"].(string); partType != "output_text" {
				return "", false, false, t
			}
			if text, ok := part["text"].(string); ok {
				return text, false, true, t
			}
			return "", false, true, t
		default:
			return "", false, false, t
		}
	}
	if delta, ok := doc["delta"].(map[string]interface{}); ok {
		if text, ok := delta["text"].(string); ok {
			return text, false, false, "fallback.delta_text"
		}
	}
	if delta, ok := doc["delta"].(string); ok {
		return delta, false, false, "fallback.delta"
	}
	if text, ok := doc["text"].(string); ok {
		return text, false, false, "fallback.text"
	}
	return "", false, false, ""
}

// runResponsesStream implemented after helper to avoid forward ref
func runResponsesStream(ctx context.Context, rc RunContext) Result {
	result := baseResult("responses.stream", "Responses streaming", rc)
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, "responses.stream")
	headers := cloneHeaders(requestHeadersForTest(rc.Config, rc.Profile, "responses.stream"))
	headers["Accept"] = "text/event-stream"
	applyStreamTimeoutHeader(headers, rc.Config, rc.Profile, "responses.stream", ov)
	payload := baseResponsesPayload(rc.Profile)
	applyResponsesReasoningOverride(payload, effectiveResponsesReasoningEffort(rc.Profile, ov))
	if maxOutputTokens := effectiveResponsesMaxOutputTokens(ov); maxOutputTokens != nil {
		payload["max_output_tokens"] = *maxOutputTokens
	}
	payload["stream"] = true
	payload["input"] = responsesInstructionInput(rc.Config, rc.Profile, "responses.stream", "system", "Reply with exactly HELLO and nothing else.", "go")
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
		delta, isDone, canonical, eventType := parseResponsesStreamEventDetailed(ev.Data)
		appendStreamEventType(&result, eventType)
		if delta != "" {
			if eventType != "response.output_text.done" || text.Len() == 0 {
				text.WriteString(delta)
			}
			if canonical {
				ensureEvidence(&result).CanonicalStreamTextSeen = true
			} else {
				ensureEvidence(&result).FallbackStreamTextSeen = true
			}
		}
		if isDone {
			if eventType == "response.completed" {
				ensureEvidence(&result).CanonicalStreamTerminalSeen = true
				done = true
				return sse.ErrStop
			}
			if eventType == "[DONE]" {
				done = true
				return sse.ErrStop
			}
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
	recordResponsesBodyEvidence(&result, resp.Body)
	if isEndpointMissing(resp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if isUnsupportedFeature(resp.Body) {
		return unsupportedFeatureResult(result, resp.Body)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return failHTTPStatusResult(result, resp)
	}
	if !done && (resp == nil || !resp.Done) {
		return failResult(result, errors.New("stream did not terminate"), "stream")
	}
	if strings.TrimSpace(text.String()) == "" {
		return failResult(result, errors.New("empty stream"), "stream")
	}
	if normalizeStreamText(text.String()) != "HELLO" {
		return failResult(result, errors.New("expected HELLO"), "assert")
	}
	result.Status = StatusPass
	return result
}
