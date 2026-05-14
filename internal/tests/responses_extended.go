package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const responsesExtendedTarget = "responses_extended"

func runResponsesAssistantPhase(ctx context.Context, rc RunContext) Result {
	const testID = "responses.assistant_phase"
	result := baseResult(testID, "Responses assistant phase", rc)
	headers := requestHeadersForTest(rc.Config, rc.Profile, testID)
	ov, _ := testOverrideForProfile(rc.Config, rc.Profile, testID)

	payload := baseResponsesPayload(rc.Profile)
	applyResponsesReasoningOverride(payload, effectiveResponsesReasoningEffort(rc.Profile, ov))
	payload["input"] = []map[string]interface{}{
		{
			"type":    "message",
			"role":    "assistant",
			"phase":   "commentary",
			"content": "I should answer with the saved number.",
		},
		{
			"type":    "message",
			"role":    "assistant",
			"phase":   "final_answer",
			"content": "The number is four.",
		},
		{
			"type":    "message",
			"role":    "user",
			"content": "Repeat only the number.",
		},
	}
	withTraceStep(&result, "assistant_phase_request", prettyJSON(payload), "")
	resp, err := rc.Client.PostJSON(ctx, rc.Config.Endpoints.Paths.Responses, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, resp)
	updateTraceStepResponse(&result, "assistant_phase_request", string(resp.Body))
	if blocked := maybeUnsupportedResponsesResult(result, resp); blocked != nil {
		return *blocked
	}
	if err := validateCompletedResponseBody(resp.Body); err != nil {
		return failResult(result, err, "schema")
	}
	result.Status = StatusPass
	result.ErrorType = ""
	result.ErrorMessage = ""
	return result
}

func runResponsesCompact(ctx context.Context, rc RunContext) Result {
	const testID = "responses.compact"
	result := baseResult(testID, "Responses compact", rc)
	headers := requestHeadersForTest(rc.Config, rc.Profile, testID)
	compactPath := rc.Config.Endpoints.Paths.Responses + "/compact"

	payload := map[string]interface{}{
		"model": rc.Profile.ResponsesModel,
		"input": []map[string]interface{}{
			{
				"type":    "message",
				"role":    "user",
				"content": "We agreed to launch on Tuesday and notify support first.",
			},
			{
				"type":    "message",
				"role":    "assistant",
				"content": "Understood. The launch is Tuesday, with support notified beforehand.",
			},
		},
	}
	withTraceStep(&result, "compact_response_context", prettyJSON(payload), "")
	resp, err := rc.Client.PostJSON(ctx, compactPath, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, resp)
	updateTraceStepResponse(&result, "compact_response_context", string(resp.Body))
	if blocked := maybeUnsupportedResponsesResult(result, resp); blocked != nil {
		return *blocked
	}
	if err := validateCompactionBody(resp.Body); err != nil {
		return failResult(result, err, "schema")
	}
	result.Status = StatusPass
	result.ErrorType = ""
	result.ErrorMessage = ""
	return result
}

func runResponsesCompactMissingModel(ctx context.Context, rc RunContext) Result {
	const testID = "responses.compact.missing_model"
	result := baseResult(testID, "Responses compact missing model", rc)
	headers := requestHeadersForTest(rc.Config, rc.Profile, testID)
	compactPath := rc.Config.Endpoints.Paths.Responses + "/compact"

	payload := map[string]interface{}{
		"input": []map[string]interface{}{
			{
				"type":    "message",
				"role":    "user",
				"content": "Compact this conversation.",
			},
		},
	}
	withTraceStep(&result, "compact_missing_model", prettyJSON(payload), "")
	resp, err := rc.Client.PostJSON(ctx, compactPath, payload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, resp)
	updateTraceStepResponse(&result, "compact_missing_model", string(resp.Body))
	if isEndpointMissing(resp.StatusCode) {
		return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", resp.StatusCode))
	}
	if resp.StatusCode != 400 && resp.StatusCode != 422 {
		return failHTTPStatusResult(result, resp)
	}
	if msg := strings.TrimSpace(extractErrorMessage(resp.Body)); msg == "" {
		return failResult(result, errors.New("missing error message for compact request without model"), "schema")
	}
	result.Status = StatusPass
	result.ErrorType = ""
	result.ErrorMessage = ""
	return result
}

func validateCompletedResponseBody(body []byte) error {
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return err
	}
	if obj := firstString(doc["object"]); obj != "response" {
		return fmt.Errorf("expected object response, got %q", obj)
	}
	status := firstString(doc["status"])
	if status != "" && status != "completed" {
		return fmt.Errorf("expected status completed, got %q", status)
	}
	out, ok := doc["output"].([]interface{})
	if !ok || len(out) == 0 {
		return errors.New("response has no output items")
	}
	return nil
}

func validateCompactionBody(body []byte) error {
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return err
	}
	if obj := firstString(doc["object"]); obj != "response.compaction" {
		return fmt.Errorf("expected object response.compaction, got %q", obj)
	}
	out, ok := doc["output"].([]interface{})
	if !ok || len(out) == 0 {
		return errors.New("compaction response has no output items")
	}
	for _, item := range out {
		m, _ := item.(map[string]interface{})
		if m == nil {
			continue
		}
		if firstString(m["type"]) == "compaction" {
			return nil
		}
	}
	return errors.New("compaction response did not include a compaction output item")
}

func extractCompactionOutputItems(body []byte) ([]interface{}, error) {
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	out, ok := doc["output"].([]interface{})
	if !ok || len(out) == 0 {
		return nil, errors.New("compaction response has no output items")
	}
	return out, nil
}
