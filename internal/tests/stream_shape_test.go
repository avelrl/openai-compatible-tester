package tests

import "testing"

func TestValidateResponsesStreamEventShapeOutputTextDelta(t *testing.T) {
	event := `{"type":"response.output_text.delta","sequence_number":4,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"HELLO"}`
	if errs := validateResponsesStreamEventShape(event, false); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestValidateResponsesStreamEventShapeMissingSequence(t *testing.T) {
	event := `{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"HELLO"}`
	errs := validateResponsesStreamEventShape(event, false)
	if len(errs) == 0 {
		t.Fatalf("expected missing sequence_number error")
	}
}

func TestValidateResponsesStreamEventShapeAllowsFallbackChatChunk(t *testing.T) {
	event := `{"choices":[{"delta":{"content":"HELLO"}}]}`
	if errs := validateResponsesStreamEventShape(event, false); len(errs) != 0 {
		t.Fatalf("unexpected fallback errors: %v", errs)
	}
}

func TestValidateResponsesStreamEventShapeWebSocketError(t *testing.T) {
	event := map[string]interface{}{
		"type":   "error",
		"status": 400,
		"error": map[string]interface{}{
			"code":    "previous_response_not_found",
			"message": "previous response not found",
		},
	}
	if errs := validateResponsesStreamEventDoc(event, true); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestValidateResponsesStreamEventShapeWebSocketErrorWithoutCode(t *testing.T) {
	event := map[string]interface{}{
		"type":   "error",
		"status": 400,
		"error": map[string]interface{}{
			"type":    "invalid_request_error",
			"message": "No tool call found for function call output.",
			"param":   "input",
		},
	}
	if errs := validateResponsesStreamEventDoc(event, true); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestValidateResponsesStreamEventShapeSSEError(t *testing.T) {
	event := map[string]interface{}{
		"type":            "error",
		"code":            "ERR_SOMETHING",
		"message":         "Something went wrong",
		"param":           nil,
		"sequence_number": 1,
	}
	if errs := validateResponsesStreamEventDoc(event, false); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestResponsesWebSocketURL(t *testing.T) {
	got, err := responsesWebSocketURL("https://example.test", "/v1/responses")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "wss://example.test/v1/responses" {
		t.Fatalf("got %q", got)
	}
}

func TestValidateWebSocketCreateRequestRejectsHTTPStreamFields(t *testing.T) {
	request := map[string]interface{}{
		"type":   "response.create",
		"model":  "gpt-test",
		"stream": true,
	}
	if err := validateWebSocketCreateRequest(request); err == nil {
		t.Fatalf("expected stream field rejection")
	}
}
