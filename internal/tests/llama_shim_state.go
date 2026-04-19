package tests

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/avelrl/openai-compatible-tester/internal/httpclient"
)

func runLlamaShimResponsesPreviousResponseChain(ctx context.Context, rc RunContext) Result {
	result := baseResult("responses.previous_response.chain", "Responses previous_response_id chain", rc)
	headers := requestHeadersForTest(rc.Config, rc.Profile, "responses.previous_response.chain")
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, "responses.previous_response.chain")
	primary := "1729"
	backup := "3141"

	payload1 := baseResponsesPayload(rc.Profile)
	applyResponsesReasoningOverride(payload1, effectiveResponsesReasoningEffort(rc.Profile, ov))
	payload1["store"] = true
	payload1["input"] = responsesInputWithOptionalInstruction(
		rc.Config,
		rc.Profile,
		"responses.previous_response.chain",
		"",
		fmt.Sprintf("Remember the primary code %s. Reply OK.", primary),
		"",
		fmt.Sprintf("Remember the primary code %s. Reply OK.", primary),
	)
	withTraceStep(&result, "chain_turn_1", prettyJSON(payload1), "")
	resp1, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, payload1, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, resp1)
	updateTraceStepResponse(&result, "chain_turn_1", string(resp1.Body))
	if blocked := maybeUnsupportedResponsesResult(result, resp1); blocked != nil {
		return *blocked
	}
	id1 := extractResponseID(resp1.Body)
	if id1 == "" {
		return failResult(result, errors.New("missing first response id"), "schema")
	}

	payload2 := baseResponsesPayload(rc.Profile)
	applyResponsesReasoningOverride(payload2, effectiveResponsesReasoningEffort(rc.Profile, ov))
	payload2["store"] = true
	payload2["previous_response_id"] = id1
	payload2["input"] = fmt.Sprintf("Also remember the backup code %s. Reply OK.", backup)
	withTraceStep(&result, "chain_turn_2", prettyJSON(payload2), "")
	resp2, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, payload2, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, resp2)
	updateTraceStepResponse(&result, "chain_turn_2", string(resp2.Body))
	if blocked := maybeUnsupportedResponsesResult(result, resp2); blocked != nil {
		return *blocked
	}
	if extractPreviousResponseID(resp2.Body) == "" {
		return unsupportedResult(result, "stateless_responses", "previous_response_id was ignored on the second turn")
	}
	id2 := extractResponseID(resp2.Body)
	if id2 == "" {
		return failResult(result, errors.New("missing second response id"), "schema")
	}

	payload3 := baseResponsesPayload(rc.Profile)
	applyResponsesReasoningOverride(payload3, effectiveResponsesReasoningEffort(rc.Profile, ov))
	payload3["store"] = true
	payload3["previous_response_id"] = id2
	payload3["input"] = fmt.Sprintf("Reply with the primary code and backup code in order as digits only, separated by a single space. Expected answer: %s %s", primary, backup)
	withTraceStep(&result, "chain_turn_3", prettyJSON(payload3), "")
	resp3, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, payload3, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, resp3)
	updateTraceStepResponse(&result, "chain_turn_3", string(resp3.Body))
	if blocked := maybeUnsupportedResponsesResult(result, resp3); blocked != nil {
		return *blocked
	}
	if extractPreviousResponseID(resp3.Body) == "" {
		return unsupportedResult(result, "stateless_responses", "previous_response_id was ignored on the third turn")
	}
	text := strings.TrimSpace(extractResponseText(resp3.Body))
	if text == "" {
		return failResult(result, errors.New("missing final chain text"), "schema")
	}
	if !containsOrderedTokens(text, primary, backup) {
		return failResult(result, fmt.Errorf("expected ordered codes %s then %s, got %q", primary, backup, text), "assert")
	}
	result.Status = StatusPass
	result.ErrorType = ""
	result.ErrorMessage = ""
	return result
}

func runLlamaShimResponsesRetrieveInputItems(ctx context.Context, rc RunContext) Result {
	result := baseResult("responses.retrieve.input_items", "Responses retrieve input_items", rc)
	headers := requestHeadersForTest(rc.Config, rc.Profile, "responses.retrieve.input_items")
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, "responses.retrieve.input_items")
	sentinel := "llama-shim-input-items-sentinel-2049"

	payload := baseResponsesPayload(rc.Profile)
	applyResponsesReasoningOverride(payload, effectiveResponsesReasoningEffort(rc.Profile, ov))
	payload["store"] = true
	payload["input"] = responsesInstructionInput(
		rc.Config,
		rc.Profile,
		"responses.retrieve.input_items",
		"system",
		"You are a test assistant. Reply OK.",
		"Repeat nothing. Just acknowledge this marker internally: "+sentinel,
	)
	withTraceStep(&result, "create_response_for_input_items", prettyJSON(payload), "")
	resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, resp)
	updateTraceStepResponse(&result, "create_response_for_input_items", string(resp.Body))
	if blocked := maybeUnsupportedResponsesResult(result, resp); blocked != nil {
		return *blocked
	}
	id := extractResponseID(resp.Body)
	if id == "" {
		return failResult(result, errors.New("missing response id"), "schema")
	}

	getPath := rc.Config.Endpoints.Paths.Responses + "/" + id + "/input_items"
	result.RequestSnippet = clip("GET "+getPath, snippetLimit)
	withTraceStep(&result, "get_input_items", "GET "+getPath, "")
	getResp, err := rc.Client.Get(ctx, getPath, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, getResp)
	updateTraceStepResponse(&result, "get_input_items", string(getResp.Body))
	if isHTMLDocument(getResp.Body) {
		return unsupportedResult(result, "unsupported_get", "html page returned instead of API response")
	}
	if isGetUnsupported(getResp.Body) || isUnexpectedEndpointOrMethod(getResp.Body) {
		msg := errorMessage(getResp.Body)
		if strings.TrimSpace(msg) == "" {
			msg = fmt.Sprintf("status %d", getResp.StatusCode)
		}
		return unsupportedResult(result, "unsupported_get", msg)
	}
	if isEndpointMissing(getResp.StatusCode) {
		return unsupportedResult(result, "unsupported_get", fmt.Sprintf("status %d", getResp.StatusCode))
	}
	if getResp.StatusCode < 200 || getResp.StatusCode >= 300 {
		return failHTTPStatusResult(result, getResp)
	}
	if !strings.Contains(string(getResp.Body), sentinel) {
		return failResult(result, fmt.Errorf("input_items response did not include sentinel %q", sentinel), "schema")
	}
	result.Status = StatusPass
	result.ErrorType = ""
	result.ErrorMessage = ""
	return result
}

func runLlamaShimConversationsCreateRetrieve(ctx context.Context, rc RunContext) Result {
	result := baseResult("conversations.create.retrieve", "Conversations create + GET", rc)
	headers := requestHeadersForTest(rc.Config, rc.Profile, "conversations.create.retrieve")
	sentinel := "llama-shim-conversation-retrieve-7781"
	payload := map[string]interface{}{
		"metadata": map[string]string{
			"suite":    "llama_shim",
			"sentinel": sentinel,
		},
		"items": []map[string]string{
			responsesConversationInstructionItem(rc.Config, rc.Profile, "conversations.create.retrieve", "system", "You are a test assistant."),
			{"type": "message", "role": "user", "content": "Hello from " + sentinel},
		},
	}
	withTraceStep(&result, "create_conversation_for_get", prettyJSON(payload), "")
	resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Conversations, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, resp)
	updateTraceStepResponse(&result, "create_conversation_for_get", string(resp.Body))
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

	getPath := rc.Config.Endpoints.Paths.Conversations + "/" + convID
	result.RequestSnippet = clip("GET "+getPath, snippetLimit)
	withTraceStep(&result, "get_conversation", "GET "+getPath, "")
	getResp, err := rc.Client.Get(ctx, getPath, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, getResp)
	updateTraceStepResponse(&result, "get_conversation", string(getResp.Body))
	if isHTMLDocument(getResp.Body) {
		return unsupportedResult(result, "unsupported_get", "html page returned instead of API response")
	}
	if isUnexpectedEndpointOrMethod(getResp.Body) {
		return unsupportedResult(result, "unsupported_get", errorMessage(getResp.Body))
	}
	if isEndpointMissing(getResp.StatusCode) {
		return unsupportedResult(result, "unsupported_get", fmt.Sprintf("status %d", getResp.StatusCode))
	}
	if getResp.StatusCode < 200 || getResp.StatusCode >= 300 {
		return failHTTPStatusResult(result, getResp)
	}
	body := string(getResp.Body)
	if !strings.Contains(body, convID) {
		return failResult(result, fmt.Errorf("retrieved conversation did not include id %q", convID), "schema")
	}
	if !strings.Contains(body, sentinel) {
		return failResult(result, fmt.Errorf("retrieved conversation did not include sentinel %q", sentinel), "schema")
	}
	result.Status = StatusPass
	result.ErrorType = ""
	result.ErrorMessage = ""
	return result
}

func recordHTTPResponse(result *Result, resp *httpclient.Response) {
	if result == nil || resp == nil {
		return
	}
	result.HTTPStatus = resp.StatusCode
	result.LatencyMS = resp.Latency.Milliseconds()
	result.BytesIn = resp.BytesIn
	result.BytesOut = resp.BytesOut
	result.ResponseSnippet = clip(string(resp.Body), snippetLimit)
	recordResponsesBodyEvidence(result, resp.Body)
}

func maybeUnsupportedResponsesResult(result Result, resp *httpclient.Response) *Result {
	if resp == nil {
		return nil
	}
	if isHTMLDocument(resp.Body) {
		out := unsupportedResult(result, "endpoint_missing", "html page returned instead of API response")
		return &out
	}
	if isUnexpectedEndpointOrMethod(resp.Body) {
		out := unsupportedResult(result, "endpoint_missing", errorMessage(resp.Body))
		return &out
	}
	if isEndpointMissing(resp.StatusCode) {
		out := unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
		return &out
	}
	if isUnsupportedFeature(resp.Body) {
		out := unsupportedFeatureResult(result, resp.Body)
		return &out
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		out := failHTTPStatusResult(result, resp)
		return &out
	}
	return nil
}

func containsOrderedTokens(text string, tokens ...string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	start := 0
	for _, token := range tokens {
		idx := strings.Index(normalized[start:], strings.ToLower(strings.TrimSpace(token)))
		if idx < 0 {
			return false
		}
		start += idx + len(strings.TrimSpace(token))
	}
	return true
}
