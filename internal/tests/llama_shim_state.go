package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
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

func runLlamaShimConversationsItemsList(ctx context.Context, rc RunContext) Result {
	result := baseResult("conversations.items.list", "Conversations list items", rc)
	headers := requestHeadersForTest(rc.Config, rc.Profile, "conversations.items.list")
	sentinel := "llama-shim-conversation-items-list-8317"
	createPayload := map[string]interface{}{
		"items": []map[string]interface{}{
			inputTextMessageItem("system", "You are a test assistant."),
			inputTextMessageItem("user", "Seed marker "+sentinel),
		},
	}
	withTraceStep(&result, "create_conversation_for_items_list", prettyJSON(createPayload), "")
	createResp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Conversations, createPayload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, createResp)
	updateTraceStepResponse(&result, "create_conversation_for_items_list", string(createResp.Body))
	if isHTMLDocument(createResp.Body) {
		return unsupportedResult(result, "endpoint_missing", "html page returned instead of API response")
	}
	if isUnexpectedEndpointOrMethod(createResp.Body) {
		return unsupportedResult(result, "endpoint_missing", errorMessage(createResp.Body))
	}
	if isEndpointMissing(createResp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", createResp.StatusCode))
	}
	if createResp.StatusCode < 200 || createResp.StatusCode >= 300 {
		return failHTTPStatusResult(result, createResp)
	}
	convID := extractResponseID(createResp.Body)
	if convID == "" {
		return failResult(result, errors.New("missing conversation id"), "schema")
	}

	listPath := rc.Config.Endpoints.Paths.Conversations + "/" + url.PathEscape(convID) + "/items?order=asc&limit=20"
	result.RequestSnippet = clip("GET "+listPath, snippetLimit)
	withTraceStep(&result, "list_conversation_items", "GET "+listPath, "")
	listResp, err := rc.Client.Get(ctx, listPath, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, listResp)
	updateTraceStepResponse(&result, "list_conversation_items", string(listResp.Body))
	if isHTMLDocument(listResp.Body) {
		return unsupportedResult(result, "unsupported_get", "html page returned instead of API response")
	}
	if isUnexpectedEndpointOrMethod(listResp.Body) {
		return unsupportedResult(result, "unsupported_get", errorMessage(listResp.Body))
	}
	if isEndpointMissing(listResp.StatusCode) {
		return unsupportedResult(result, "unsupported_get", fmt.Sprintf("status %d", listResp.StatusCode))
	}
	if listResp.StatusCode < 200 || listResp.StatusCode >= 300 {
		return failHTTPStatusResult(result, listResp)
	}
	itemIDs, err := extractListItemIDs(listResp.Body)
	if err != nil {
		return failResult(result, err, "schema")
	}
	if len(itemIDs) == 0 {
		return failResult(result, errors.New("conversation items list returned no ids"), "schema")
	}
	body := string(listResp.Body)
	if !strings.Contains(body, sentinel) {
		return failResult(result, fmt.Errorf("conversation items list did not include sentinel %q", sentinel), "schema")
	}
	result.Status = StatusPass
	result.ErrorType = ""
	result.ErrorMessage = ""
	return result
}

func runLlamaShimConversationsItemsAppend(ctx context.Context, rc RunContext) Result {
	result := baseResult("conversations.items.append", "Conversations append items", rc)
	headers := requestHeadersForTest(rc.Config, rc.Profile, "conversations.items.append")
	seed := "llama-shim-conversation-items-seed-4081"
	firstAppend := "llama-shim-conversation-items-append-a-4081"
	secondAppend := "llama-shim-conversation-items-append-b-4081"

	createPayload := map[string]interface{}{
		"items": []map[string]interface{}{
			inputTextMessageItem("system", "You are a test assistant."),
			inputTextMessageItem("user", "Seed marker "+seed),
		},
	}
	withTraceStep(&result, "create_conversation_for_append", prettyJSON(createPayload), "")
	createResp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Conversations, createPayload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, createResp)
	updateTraceStepResponse(&result, "create_conversation_for_append", string(createResp.Body))
	if isHTMLDocument(createResp.Body) {
		return unsupportedResult(result, "endpoint_missing", "html page returned instead of API response")
	}
	if isUnexpectedEndpointOrMethod(createResp.Body) {
		return unsupportedResult(result, "endpoint_missing", errorMessage(createResp.Body))
	}
	if isEndpointMissing(createResp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", createResp.StatusCode))
	}
	if createResp.StatusCode < 200 || createResp.StatusCode >= 300 {
		return failHTTPStatusResult(result, createResp)
	}
	convID := extractResponseID(createResp.Body)
	if convID == "" {
		return failResult(result, errors.New("missing conversation id"), "schema")
	}

	appendPath := rc.Config.Endpoints.Paths.Conversations + "/" + url.PathEscape(convID) + "/items"
	appendPayload := map[string]interface{}{
		"items": []map[string]interface{}{
			inputTextMessageItem("user", "Append marker "+firstAppend),
			inputTextMessageItem("user", "Append marker "+secondAppend),
		},
	}
	withTraceStep(&result, "append_conversation_items", prettyJSON(appendPayload), "")
	appendResp, err := rc.Client.PostJSON(ctx, appendPath, appendPayload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, appendResp)
	updateTraceStepResponse(&result, "append_conversation_items", string(appendResp.Body))
	if isHTMLDocument(appendResp.Body) {
		return unsupportedResult(result, "unsupported_post", "html page returned instead of API response")
	}
	if isUnexpectedEndpointOrMethod(appendResp.Body) {
		return unsupportedResult(result, "unsupported_post", errorMessage(appendResp.Body))
	}
	if isEndpointMissing(appendResp.StatusCode) {
		return unsupportedResult(result, "unsupported_post", fmt.Sprintf("status %d", appendResp.StatusCode))
	}
	if appendResp.StatusCode < 200 || appendResp.StatusCode >= 300 {
		return failHTTPStatusResult(result, appendResp)
	}
	appendBody := string(appendResp.Body)
	if !strings.Contains(appendBody, firstAppend) || !strings.Contains(appendBody, secondAppend) {
		return failResult(result, errors.New("append response did not include appended sentinels"), "schema")
	}
	itemIDs, err := extractListItemIDs(appendResp.Body)
	if err != nil {
		return failResult(result, err, "schema")
	}
	if len(itemIDs) < 2 {
		return failResult(result, fmt.Errorf("expected at least 2 appended item ids, got %d", len(itemIDs)), "schema")
	}
	firstItemID, err := extractListItemIDContaining(appendResp.Body, firstAppend)
	if err != nil {
		return failResult(result, err, "schema")
	}

	getItemPath := rc.Config.Endpoints.Paths.Conversations + "/" + url.PathEscape(convID) + "/items/" + url.PathEscape(firstItemID)
	result.RequestSnippet = clip("GET "+getItemPath, snippetLimit)
	withTraceStep(&result, "get_appended_item", "GET "+getItemPath, "")
	getResp, err := rc.Client.Get(ctx, getItemPath, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, getResp)
	updateTraceStepResponse(&result, "get_appended_item", string(getResp.Body))
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
	getBody := string(getResp.Body)
	if !strings.Contains(getBody, firstItemID) {
		return failResult(result, fmt.Errorf("conversation item get did not include id %q", firstItemID), "schema")
	}
	if !strings.Contains(getBody, firstAppend) {
		return failResult(result, fmt.Errorf("conversation item get did not include first appended sentinel %q", firstAppend), "schema")
	}
	result.Status = StatusPass
	result.ErrorType = ""
	result.ErrorMessage = ""
	return result
}

func runLlamaShimResponsesCompaction(ctx context.Context, rc RunContext) Result {
	result := baseResult("responses.compaction", "Responses compaction", rc)
	headers := requestHeadersForTest(rc.Config, rc.Profile, "responses.compaction")
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, "responses.compaction")
	compactPath := rc.Config.Endpoints.Paths.Responses + "/compact"
	code := "777"

	compactPayload := map[string]interface{}{
		"model": rc.Profile.ResponsesModel,
		"input": []map[string]interface{}{
			inputTextMessageItem("system", "You are a test assistant. Remember the launch code for the next turn."),
			inputTextMessageItem("user", "Remember that the launch code is "+code+"."),
			outputTextMessageItem("assistant", "I will remember the launch code."),
		},
	}
	withTraceStep(&result, "compact_response_context", prettyJSON(compactPayload), "")
	compactResp, err := rc.Client.PostJSON(ctx, compactPath, compactPayload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, compactResp)
	updateTraceStepResponse(&result, "compact_response_context", string(compactResp.Body))
	if blocked := maybeUnsupportedResponsesResult(result, compactResp); blocked != nil {
		return *blocked
	}
	if !strings.Contains(string(compactResp.Body), "\"object\":\"response.compaction\"") {
		return failResult(result, errors.New("compaction endpoint did not return response.compaction"), "schema")
	}
	blob, err := extractCompactionEncryptedContent(compactResp.Body)
	if err != nil {
		return failResult(result, err, "schema")
	}
	if !strings.HasPrefix(blob, "llama_shim.compaction.") {
		return failResult(result, fmt.Errorf("unexpected compaction blob prefix for %q", blob), "schema")
	}

	followupPayload := baseResponsesPayload(rc.Profile)
	applyResponsesReasoningOverride(followupPayload, effectiveResponsesReasoningEffort(rc.Profile, ov))
	followupPayload["store"] = true
	followupPayload["input"] = []map[string]interface{}{
		{"type": "compaction", "encrypted_content": blob},
		inputTextMessageItem("user", "What is the launch code? Reply with just the number."),
	}
	withTraceStep(&result, "use_compaction_blob", prettyJSON(followupPayload), "")
	followupResp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, followupPayload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, followupResp)
	updateTraceStepResponse(&result, "use_compaction_blob", string(followupResp.Body))
	if blocked := maybeUnsupportedResponsesResult(result, followupResp); blocked != nil {
		return *blocked
	}
	text := strings.TrimSpace(extractResponseText(followupResp.Body))
	if text == "" {
		return failResult(result, errors.New("missing follow-up text after compaction"), "schema")
	}
	if !strings.Contains(text, code) {
		return failResult(result, fmt.Errorf("expected compaction follow-up to mention %s, got %q", code, text), "assert")
	}
	result.Status = StatusPass
	result.ErrorType = ""
	result.ErrorMessage = ""
	return result
}

func runLlamaShimResponsesAutoCompaction(ctx context.Context, rc RunContext) Result {
	result := baseResult("responses.compaction.auto", "Responses auto compaction", rc)
	headers := requestHeadersForTest(rc.Config, rc.Profile, "responses.compaction.auto")
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, "responses.compaction.auto")
	code := "2468"

	seedPayload := baseResponsesPayload(rc.Profile)
	applyResponsesReasoningOverride(seedPayload, effectiveResponsesReasoningEffort(rc.Profile, ov))
	seedPayload["store"] = true
	seedPayload["input"] = []map[string]interface{}{
		inputTextMessageItem("system", "You are a test assistant."),
		inputTextMessageItem("user", "Remember that the auto compaction code is "+code+". Reply with OK only."),
	}
	withTraceStep(&result, "seed_auto_compaction_history", prettyJSON(seedPayload), "")
	seedResp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, seedPayload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, seedResp)
	updateTraceStepResponse(&result, "seed_auto_compaction_history", string(seedResp.Body))
	if blocked := maybeUnsupportedResponsesResult(result, seedResp); blocked != nil {
		return *blocked
	}
	seedResponseID := extractResponseID(seedResp.Body)
	if seedResponseID == "" {
		return failResult(result, errors.New("missing seed response id for auto compaction"), "schema")
	}

	payload := baseResponsesPayload(rc.Profile)
	applyResponsesReasoningOverride(payload, effectiveResponsesReasoningEffort(rc.Profile, ov))
	payload["store"] = true
	payload["previous_response_id"] = seedResponseID
	payload["context_management"] = []map[string]interface{}{
		{"type": "compaction", "compact_threshold": 1},
	}
	payload["input"] = []map[string]interface{}{
		inputTextMessageItem("user", "Acknowledge that you still remember the auto compaction code. Reply with OK only."),
	}
	withTraceStep(&result, "create_auto_compaction_response", prettyJSON(payload), "")
	resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, resp)
	updateTraceStepResponse(&result, "create_auto_compaction_response", string(resp.Body))
	if blocked := maybeUnsupportedResponsesResult(result, resp); blocked != nil {
		return *blocked
	}
	outputTypes, err := extractOutputItemTypes(resp.Body)
	if err != nil {
		return failResult(result, err, "schema")
	}
	if len(outputTypes) == 0 || outputTypes[0] != "compaction" {
		return failResult(result, fmt.Errorf("expected prepended compaction item, got output types %v", outputTypes), "schema")
	}
	if _, err := extractCompactionEncryptedContent(resp.Body); err != nil {
		return failResult(result, err, "schema")
	}
	responseID := extractResponseID(resp.Body)
	if responseID == "" {
		return failResult(result, errors.New("missing response id for auto compaction follow-up"), "schema")
	}

	followupPayload := baseResponsesPayload(rc.Profile)
	applyResponsesReasoningOverride(followupPayload, effectiveResponsesReasoningEffort(rc.Profile, ov))
	followupPayload["previous_response_id"] = responseID
	followupPayload["input"] = "What is the auto compaction code? Reply with just the number."
	withTraceStep(&result, "follow_auto_compaction_response", prettyJSON(followupPayload), "")
	followupResp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, followupPayload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, followupResp)
	updateTraceStepResponse(&result, "follow_auto_compaction_response", string(followupResp.Body))
	if blocked := maybeUnsupportedResponsesResult(result, followupResp); blocked != nil {
		return *blocked
	}
	text := strings.TrimSpace(extractResponseText(followupResp.Body))
	if text == "" {
		return failResult(result, errors.New("missing auto compaction follow-up text"), "schema")
	}
	if !strings.Contains(text, code) {
		return failResult(result, fmt.Errorf("expected auto compaction follow-up to mention %s, got %q", code, text), "assert")
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

func inputTextMessageItem(role, text string) map[string]interface{} {
	return map[string]interface{}{
		"type": "message",
		"role": role,
		"content": []map[string]string{
			{"type": "input_text", "text": text},
		},
	}
}

func outputTextMessageItem(role, text string) map[string]interface{} {
	return map[string]interface{}{
		"type": "message",
		"role": role,
		"content": []map[string]string{
			{"type": "output_text", "text": text},
		},
	}
}

func extractListItemIDs(body []byte) ([]string, error) {
	var doc struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(doc.Data))
	for _, item := range doc.Data {
		id := strings.TrimSpace(item.ID)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func extractListItemIDContaining(body []byte, needle string) (string, error) {
	var doc struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", err
	}
	for _, item := range doc.Data {
		itemBody, err := json.Marshal(item)
		if err != nil {
			return "", err
		}
		if !strings.Contains(string(itemBody), needle) {
			continue
		}
		if id, _ := item["id"].(string); strings.TrimSpace(id) != "" {
			return strings.TrimSpace(id), nil
		}
	}
	return "", fmt.Errorf("missing item id for entry containing %q", needle)
}

func extractCompactionEncryptedContent(body []byte) (string, error) {
	var doc struct {
		Output []struct {
			Type             string `json:"type"`
			EncryptedContent string `json:"encrypted_content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", err
	}
	for _, item := range doc.Output {
		if strings.TrimSpace(item.Type) != "compaction" {
			continue
		}
		blob := strings.TrimSpace(item.EncryptedContent)
		if blob != "" {
			return blob, nil
		}
	}
	return "", errors.New("missing compaction encrypted_content")
}

func extractOutputItemTypes(body []byte) ([]string, error) {
	var doc struct {
		Output []struct {
			Type string `json:"type"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	types := make([]string, 0, len(doc.Output))
	for _, item := range doc.Output {
		itemType := strings.TrimSpace(item.Type)
		if itemType != "" {
			types = append(types, itemType)
		}
	}
	return types, nil
}
