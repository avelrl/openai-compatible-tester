package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nhooyr.io/websocket"
)

type webSocketRequestStep func(previousTurns []webSocketTurnResult) (map[string]interface{}, error)

type webSocketTurnResult struct {
	Request       map[string]interface{}
	Events        []map[string]interface{}
	Errors        []string
	FinalResponse map[string]interface{}
	ErrorCode     string
	ErrorEvent    map[string]interface{}
	RawMessages   []string
}

type webSocketSessionResult struct {
	Turns    []webSocketTurnResult
	Latency  time.Duration
	BytesOut int64
	BytesIn  int64
}

type webSocketHTTPError struct {
	StatusCode int
	Body       []byte
	Err        error
}

func (e *webSocketHTTPError) Error() string {
	if e == nil {
		return ""
	}
	msg := ""
	if len(e.Body) > 0 {
		msg = strings.TrimSpace(errorMessage(e.Body))
	}
	if msg == "" && e.Err != nil {
		msg = e.Err.Error()
	}
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	return fmt.Sprintf("websocket upgrade failed with HTTP %d: %s", e.StatusCode, msg)
}

func runResponsesWebSocketBasic(ctx context.Context, rc RunContext) Result {
	const testID = "responses.websocket.basic"
	result := baseResult(testID, "Responses WebSocket basic", rc)
	request := map[string]interface{}{
		"type":  "response.create",
		"model": rc.Profile.ResponsesModel,
		"input": "Reply with exactly HELLO and nothing else.",
	}
	session, err := makeResponsesWebSocketSession(ctx, rc, testID, []webSocketRequestStep{staticWebSocketRequest(request)}, true)
	if err != nil {
		return handleWebSocketSessionError(result, err)
	}
	recordWebSocketSession(&result, session)
	if len(session.Turns) != 1 {
		return failResult(result, errors.New("missing WebSocket turn"), "websocket")
	}
	if err := validateWebSocketCompletedTurn(session.Turns[0], true); err != nil {
		return failResult(result, err, "schema")
	}
	result.Status = StatusPass
	result.ErrorType = ""
	result.ErrorMessage = ""
	return result
}

func runResponsesWebSocketSequential(ctx context.Context, rc RunContext) Result {
	const testID = "responses.websocket.sequential"
	result := baseResult(testID, "Responses WebSocket sequential", rc)
	requests := []map[string]interface{}{
		{
			"type":  "response.create",
			"model": rc.Profile.ResponsesModel,
			"store": false,
			"input": "Reply with exactly: first",
		},
		{
			"type":  "response.create",
			"model": rc.Profile.ResponsesModel,
			"store": false,
			"input": "Reply with exactly: second",
		},
	}
	session, err := makeResponsesWebSocketSession(ctx, rc, testID, []webSocketRequestStep{
		staticWebSocketRequest(requests[0]),
		staticWebSocketRequest(requests[1]),
	}, true)
	if err != nil {
		return handleWebSocketSessionError(result, err)
	}
	recordWebSocketSession(&result, session)
	if len(session.Turns) != len(requests) {
		return failResult(result, fmt.Errorf("expected %d WebSocket turns, got %d", len(requests), len(session.Turns)), "websocket")
	}
	for i, turn := range session.Turns {
		if err := validateWebSocketCompletedTurn(turn, true); err != nil {
			return failResult(result, fmt.Errorf("turn %d: %w", i+1, err), "schema")
		}
	}
	result.Status = StatusPass
	result.ErrorType = ""
	result.ErrorMessage = ""
	return result
}

func runResponsesWebSocketContinuation(ctx context.Context, rc RunContext) Result {
	const testID = "responses.websocket.continuation"
	result := baseResult(testID, "Responses WebSocket continuation", rc)
	firstRequest := map[string]interface{}{
		"type":  "response.create",
		"model": rc.Profile.ResponsesModel,
		"store": false,
		"input": "Remember the code word: cobalt. Reply with OK.",
	}
	session, err := makeResponsesWebSocketSession(ctx, rc, testID, []webSocketRequestStep{
		staticWebSocketRequest(firstRequest),
		func(turns []webSocketTurnResult) (map[string]interface{}, error) {
			previousResponseID := responseIDFromTurn(turns, 0)
			if previousResponseID == "" {
				return nil, errors.New("first WebSocket turn did not return a response id")
			}
			return map[string]interface{}{
				"type":                 "response.create",
				"model":                rc.Profile.ResponsesModel,
				"store":                false,
				"previous_response_id": previousResponseID,
				"input":                "What is the code word? Reply with only the code word.",
			}, nil
		},
	}, true)
	if err != nil {
		return handleWebSocketSessionError(result, err)
	}
	recordWebSocketSession(&result, session)
	if len(session.Turns) != 2 {
		return failResult(result, fmt.Errorf("expected 2 WebSocket turns, got %d", len(session.Turns)), "websocket")
	}
	for i, turn := range session.Turns {
		if err := validateWebSocketCompletedTurn(turn, true); err != nil {
			return failResult(result, fmt.Errorf("turn %d: %w", i+1, err), "schema")
		}
	}
	result.Status = StatusPass
	result.ErrorType = ""
	result.ErrorMessage = ""
	return result
}

func runResponsesWebSocketPreviousResponseNotFound(ctx context.Context, rc RunContext) Result {
	const testID = "responses.websocket.previous_response_not_found"
	result := baseResult(testID, "Responses WebSocket missing previous_response_id", rc)
	request := map[string]interface{}{
		"type":                 "response.create",
		"model":                rc.Profile.ResponsesModel,
		"store":                false,
		"previous_response_id": fmt.Sprintf("resp_openresponses_missing_%d", time.Now().UnixNano()),
		"input":                "This should fail because the previous response is missing.",
	}
	session, err := makeResponsesWebSocketSession(ctx, rc, testID, []webSocketRequestStep{staticWebSocketRequest(request)}, true)
	if err != nil {
		return handleWebSocketSessionError(result, err)
	}
	recordWebSocketSession(&result, session)
	if len(session.Turns) != 1 {
		return failResult(result, errors.New("missing WebSocket turn"), "websocket")
	}
	if err := validateWebSocketErrorCode(session.Turns[0], "previous_response_not_found"); err != nil {
		return failResult(result, err, "assert")
	}
	result.Status = StatusPass
	result.ErrorType = ""
	result.ErrorMessage = ""
	return result
}

func runResponsesWebSocketReconnectStoreFalseRecovery(ctx context.Context, rc RunContext) Result {
	const testID = "responses.websocket.reconnect_store_false_recovery"
	result := baseResult(testID, "Responses WebSocket store:false reconnect recovery", rc)
	firstRequest := map[string]interface{}{
		"type":  "response.create",
		"model": rc.Profile.ResponsesModel,
		"store": false,
		"input": "Remember the code word: copper. Reply with OK.",
	}
	firstSession, err := makeResponsesWebSocketSession(ctx, rc, testID, []webSocketRequestStep{staticWebSocketRequest(firstRequest)}, true)
	if err != nil {
		return handleWebSocketSessionError(result, err)
	}
	recordWebSocketSession(&result, firstSession)
	if len(firstSession.Turns) != 1 {
		return failResult(result, errors.New("missing first WebSocket turn"), "websocket")
	}
	if err := validateWebSocketCompletedTurn(firstSession.Turns[0], true); err != nil {
		return failResult(result, err, "schema")
	}
	previousResponseID := responseIDFromTurn(firstSession.Turns, 0)
	if previousResponseID == "" {
		return failResult(result, errors.New("first WebSocket turn did not return a response id"), "schema")
	}

	reconnectRequest := map[string]interface{}{
		"type":                 "response.create",
		"model":                rc.Profile.ResponsesModel,
		"store":                false,
		"previous_response_id": previousResponseID,
		"input":                "Try to continue after reconnect. Reply with exactly: reconnected",
	}
	recoveryRequest := map[string]interface{}{
		"type":  "response.create",
		"model": rc.Profile.ResponsesModel,
		"store": false,
		"input": []map[string]interface{}{
			{
				"type":    "message",
				"role":    "user",
				"content": "Start a clean response and reply with exactly: recovered",
			},
		},
	}
	secondSession, err := makeResponsesWebSocketSession(ctx, rc, testID, []webSocketRequestStep{
		staticWebSocketRequest(reconnectRequest),
		staticWebSocketRequest(recoveryRequest),
	}, true)
	if err != nil {
		return handleWebSocketSessionError(result, err)
	}
	recordWebSocketSession(&result, secondSession)
	if len(secondSession.Turns) != 2 {
		return failResult(result, fmt.Errorf("expected 2 reconnect WebSocket turns, got %d", len(secondSession.Turns)), "websocket")
	}
	if err := validateWebSocketErrorCode(secondSession.Turns[0], "previous_response_not_found"); err != nil {
		return failResult(result, err, "assert")
	}
	if err := validateWebSocketCompletedTurn(secondSession.Turns[1], true); err != nil {
		return failResult(result, err, "schema")
	}
	result.Status = StatusPass
	result.ErrorType = ""
	result.ErrorMessage = ""
	return result
}

func runResponsesWebSocketFailedContinuationEvictsCache(ctx context.Context, rc RunContext) Result {
	const testID = "responses.websocket.failed_continuation_evicts_cache"
	result := baseResult(testID, "Responses WebSocket failed continuation evicts cache", rc)
	firstRequest := map[string]interface{}{
		"type":  "response.create",
		"model": rc.Profile.ResponsesModel,
		"store": false,
		"input": "Remember the code word: ember. Reply with OK.",
	}
	session, err := makeResponsesWebSocketSession(ctx, rc, testID, []webSocketRequestStep{
		staticWebSocketRequest(firstRequest),
		func(turns []webSocketTurnResult) (map[string]interface{}, error) {
			previousResponseID := responseIDFromTurn(turns, 0)
			if previousResponseID == "" {
				return nil, errors.New("first WebSocket turn did not return a response id")
			}
			return map[string]interface{}{
				"type":                 "response.create",
				"model":                rc.Profile.ResponsesModel,
				"store":                false,
				"previous_response_id": previousResponseID,
				"input": []map[string]interface{}{
					{
						"type":    "function_call_output",
						"call_id": "call_openresponses_missing",
						"output":  "No matching tool call exists in the previous response.",
					},
				},
			}, nil
		},
		func(turns []webSocketTurnResult) (map[string]interface{}, error) {
			previousResponseID := responseIDFromTurn(turns, 0)
			if previousResponseID == "" {
				return nil, errors.New("first WebSocket turn did not return a response id")
			}
			return map[string]interface{}{
				"type":                 "response.create",
				"model":                rc.Profile.ResponsesModel,
				"store":                false,
				"previous_response_id": previousResponseID,
				"input":                "Try to continue after the failed turn. Reply with exactly: stale",
			}, nil
		},
	}, true)
	if err != nil {
		return handleWebSocketSessionError(result, err)
	}
	recordWebSocketSession(&result, session)
	if len(session.Turns) != 3 {
		return failResult(result, fmt.Errorf("expected 3 WebSocket turns, got %d", len(session.Turns)), "websocket")
	}
	if err := validateWebSocketCompletedTurn(session.Turns[0], true); err != nil {
		return failResult(result, err, "schema")
	}
	if !isFailedWebSocketTurn(session.Turns[1]) {
		return failResult(result, fmt.Errorf("expected second WebSocket turn to fail, got status %q", responseStatusFromTurn(session.Turns[1])), "assert")
	}
	if err := validateWebSocketErrorCode(session.Turns[2], "previous_response_not_found"); err != nil {
		return failResult(result, err, "assert")
	}
	result.Status = StatusPass
	result.ErrorType = ""
	result.ErrorMessage = ""
	return result
}

func runResponsesWebSocketCompactNewChain(ctx context.Context, rc RunContext) Result {
	const testID = "responses.websocket.compact_new_chain"
	result := baseResult(testID, "Responses WebSocket compact new chain", rc)
	headers := requestHeadersForTest(rc.Config, rc.Profile, testID)
	compactPath := rc.Config.Endpoints.Paths.Responses + "/compact"
	compactPayload := map[string]interface{}{
		"model": rc.Profile.ResponsesModel,
		"input": []map[string]interface{}{
			inputTextMessageItem("user", "Remember the compaction code word: slate."),
			outputTextMessageItem("assistant", "OK."),
			inputTextMessageItem("user", "Compress this conversation for later continuation."),
		},
	}
	withTraceStep(&result, "compact_for_websocket", prettyJSON(compactPayload), "")
	compactResp, err := rc.Client.PostJSON(ctx, compactPath, compactPayload, headers)
	if err != nil {
		return failResult(result, err, "http_error")
	}
	recordHTTPResponse(&result, compactResp)
	updateTraceStepResponse(&result, "compact_for_websocket", string(compactResp.Body))
	if blocked := maybeUnsupportedResponsesResult(result, compactResp); blocked != nil {
		return *blocked
	}
	if err := validateCompactionBody(compactResp.Body); err != nil {
		return failResult(result, err, "schema")
	}
	compactedOutput, err := extractCompactionOutputItems(compactResp.Body)
	if err != nil {
		return failResult(result, err, "schema")
	}
	input := make([]interface{}, 0, len(compactedOutput)+1)
	input = append(input, compactedOutput...)
	input = append(input, map[string]interface{}{
		"type":    "message",
		"role":    "user",
		"content": "Continue from here. Reply with exactly: compacted",
	})
	websocketRequest := map[string]interface{}{
		"type":  "response.create",
		"model": rc.Profile.ResponsesModel,
		"store": false,
		"input": input,
		"tools": []interface{}{},
	}
	session, err := makeResponsesWebSocketSession(ctx, rc, testID, []webSocketRequestStep{staticWebSocketRequest(websocketRequest)}, false)
	if err != nil {
		return handleWebSocketSessionError(result, err)
	}
	recordWebSocketSession(&result, session)
	if len(session.Turns) != 1 {
		return failResult(result, errors.New("missing WebSocket turn after compact"), "websocket")
	}
	if _, hasPrevious := websocketRequest["previous_response_id"]; hasPrevious {
		return failResult(result, errors.New("standalone compact recovery must start without previous_response_id"), "schema")
	}
	if err := validateWebSocketCompletedTurn(session.Turns[0], true); err != nil {
		return failResult(result, err, "schema")
	}
	result.Status = StatusPass
	result.ErrorType = ""
	result.ErrorMessage = ""
	return result
}

func makeResponsesWebSocketSession(ctx context.Context, rc RunContext, testID string, steps []webSocketRequestStep, validateRequests bool) (webSocketSessionResult, error) {
	var session webSocketSessionResult
	wsURL, err := responsesWebSocketURL(rc.Config.BaseURL, rc.Config.Endpoints.Paths.Responses)
	if err != nil {
		return session, err
	}
	headers := webSocketHeadersForTest(rc, testID)
	start := time.Now()
	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		var body []byte
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
			body, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
		}
		return session, &webSocketHTTPError{StatusCode: statusCode, Body: body, Err: err}
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	for i, step := range steps {
		request, err := step(session.Turns)
		if err != nil {
			return session, err
		}
		if validateRequests {
			if err := validateWebSocketCreateRequest(request); err != nil {
				return session, fmt.Errorf("request %d: %w", i+1, err)
			}
		}
		turn := webSocketTurnResult{Request: request}
		data, err := json.Marshal(request)
		if err != nil {
			return session, err
		}
		session.BytesOut += int64(len(data))
		if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
			return session, err
		}
		turn, err = readWebSocketTurn(ctx, conn, turn)
		session.Turns = append(session.Turns, turn)
		if err != nil {
			session.Latency = time.Since(start)
			return session, err
		}
		for _, raw := range turn.RawMessages {
			session.BytesIn += int64(len(raw))
		}
	}
	session.Latency = time.Since(start)
	return session, nil
}

func readWebSocketTurn(ctx context.Context, conn *websocket.Conn, turn webSocketTurnResult) (webSocketTurnResult, error) {
	for {
		messageType, data, err := conn.Read(ctx)
		if err != nil {
			return turn, err
		}
		if messageType != websocket.MessageText && messageType != websocket.MessageBinary {
			turn.Errors = append(turn.Errors, fmt.Sprintf("unexpected WebSocket message type %v", messageType))
			continue
		}
		raw := string(data)
		turn.RawMessages = append(turn.RawMessages, raw)
		if strings.TrimSpace(raw) == "[DONE]" {
			if turn.FinalResponse == nil && turn.ErrorCode == "" {
				turn.Errors = append(turn.Errors, "received [DONE] before a terminal WebSocket event")
			}
			return turn, nil
		}

		var doc map[string]interface{}
		if err := json.Unmarshal(data, &doc); err != nil {
			turn.Errors = append(turn.Errors, fmt.Sprintf("failed to parse WebSocket event data: %s", raw))
			continue
		}
		turn.Events = append(turn.Events, doc)
		eventType := strings.TrimSpace(firstString(doc["type"]))
		if shouldValidateResponsesStreamEventType(eventType) {
			for _, shapeErr := range validateResponsesStreamEventDoc(doc, true) {
				turn.Errors = append(turn.Errors, fmt.Sprintf("event validation failed for %s: %s", eventType, shapeErr))
			}
		}
		if terminal := terminalResponseFromStreamEvent(doc); terminal != nil {
			turn.FinalResponse = terminal
			return turn, nil
		}
		if code := streamingErrorCode(doc); eventType == "error" || code != "" {
			turn.ErrorCode = code
			turn.ErrorEvent = doc
			if code == "" && len(validateResponsesStreamEventDoc(doc, true)) > 0 {
				turn.Errors = append(turn.Errors, fmt.Sprintf("WebSocket error event: %s", raw))
			}
			return turn, nil
		}
	}
}

func staticWebSocketRequest(request map[string]interface{}) webSocketRequestStep {
	return func(_ []webSocketTurnResult) (map[string]interface{}, error) {
		return request, nil
	}
}

func responsesWebSocketURL(baseURL, responsesPath string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", errors.New("missing base URL for WebSocket")
	}
	if strings.TrimSpace(responsesPath) == "" {
		responsesPath = "/v1/responses"
	}
	u, err := url.Parse(baseURL + responsesPath)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported WebSocket base URL protocol %q", u.Scheme)
	}
	return u.String(), nil
}

func webSocketHeadersForTest(rc RunContext, testID string) http.Header {
	headers := http.Header{}
	for k, v := range rc.Config.Endpoints.DefaultHeaders {
		headers.Set(k, v)
	}
	for k, v := range requestHeadersForTest(rc.Config, rc.Profile, testID) {
		headers.Set(k, v)
	}
	if headers.Get("Content-Type") == "" {
		headers.Set("Content-Type", "application/json")
	}
	if rc.Config.APIKey != "" && headers.Get("Authorization") == "" {
		headers.Set("Authorization", "Bearer "+rc.Config.APIKey)
	}
	return headers
}

func validateWebSocketCreateRequest(request map[string]interface{}) error {
	if firstString(request["type"]) != "response.create" {
		return errors.New("WebSocket request type must be response.create")
	}
	if strings.TrimSpace(firstString(request["model"])) == "" {
		return errors.New("WebSocket response.create request is missing model")
	}
	for _, disallowed := range []string{"stream", "stream_options", "background"} {
		if _, ok := request[disallowed]; ok {
			return fmt.Errorf("WebSocket response.create request must not include %s", disallowed)
		}
	}
	return nil
}

func terminalResponseFromStreamEvent(doc map[string]interface{}) map[string]interface{} {
	switch firstString(doc["type"]) {
	case "response.completed", "response.failed", "response.incomplete":
	default:
		return nil
	}
	resp, _ := doc["response"].(map[string]interface{})
	return resp
}

func streamingErrorCode(doc map[string]interface{}) string {
	errObj, _ := doc["error"].(map[string]interface{})
	if errObj == nil {
		return ""
	}
	return firstString(errObj["code"])
}

func validateWebSocketCompletedTurn(turn webSocketTurnResult, requireText bool) error {
	if len(turn.Errors) > 0 {
		return errors.New(strings.Join(turn.Errors, "; "))
	}
	if turn.FinalResponse == nil {
		return errors.New("missing terminal response")
	}
	body, err := json.Marshal(turn.FinalResponse)
	if err != nil {
		return err
	}
	if err := validateCompletedResponseBody(body); err != nil {
		return err
	}
	if requireText && strings.TrimSpace(extractResponseText(body)) == "" {
		return errors.New("missing response text")
	}
	return nil
}

func validateWebSocketErrorCode(turn webSocketTurnResult, expected string) error {
	if len(turn.Errors) > 0 {
		return errors.New(strings.Join(turn.Errors, "; "))
	}
	code := turn.ErrorCode
	if code == "" && turn.FinalResponse != nil {
		code = streamingErrorCode(turn.FinalResponse)
	}
	if code != expected {
		if code == "" {
			code = "no error code"
		}
		return fmt.Errorf("expected %s but got %s", expected, code)
	}
	return nil
}

func isFailedWebSocketTurn(turn webSocketTurnResult) bool {
	if turn.ErrorEvent != nil || turn.ErrorCode != "" {
		return true
	}
	return responseStatusFromTurn(turn) == "failed"
}

func responseIDFromTurn(turns []webSocketTurnResult, index int) string {
	if index < 0 || index >= len(turns) {
		return ""
	}
	return firstString(turns[index].FinalResponse["id"])
}

func responseStatusFromTurn(turn webSocketTurnResult) string {
	if turn.FinalResponse == nil {
		return ""
	}
	return firstString(turn.FinalResponse["status"])
}

func handleWebSocketSessionError(result Result, err error) Result {
	var wsHTTP *webSocketHTTPError
	if errors.As(err, &wsHTTP) {
		result.HTTPStatus = wsHTTP.StatusCode
		result.ResponseSnippet = clip(string(wsHTTP.Body), result.snippetLimit)
		recordErrorEvidence(&result, wsHTTP.Body)
		if isEndpointMissing(wsHTTP.StatusCode) {
			return unsupportedResult(result, "endpoint_missing", fmt.Sprintf("status %d", wsHTTP.StatusCode))
		}
		if len(wsHTTP.Body) > 0 && isUnsupportedFeature(wsHTTP.Body) {
			return unsupportedFeatureResult(result, wsHTTP.Body)
		}
	}
	return failResult(result, err, "websocket")
}

func recordWebSocketSession(result *Result, session webSocketSessionResult) {
	if result == nil {
		return
	}
	result.LatencyMS = session.Latency.Milliseconds()
	result.BytesIn += session.BytesIn
	result.BytesOut += session.BytesOut
	trace := strings.Builder{}
	for i, turn := range session.Turns {
		name := fmt.Sprintf("websocket_turn_%d", i+1)
		request := prettyJSON(turn.Request)
		response := prettyJSON(map[string]interface{}{
			"events":         turn.Events,
			"final_response": turn.FinalResponse,
			"error_event":    turn.ErrorEvent,
			"errors":         turn.Errors,
		})
		withTraceStep(result, name, request, response)
		trace.WriteString("TURN ")
		trace.WriteString(fmt.Sprintf("%d", i+1))
		trace.WriteString(":\n")
		trace.WriteString(response)
		trace.WriteString("\n")

		for _, event := range turn.Events {
			eventType := strings.TrimSpace(firstString(event["type"]))
			appendStreamEventType(result, eventType)
			if shouldValidateResponsesStreamEventType(eventType) {
				appendStreamShapeErrors(result, validateResponsesStreamEventDoc(event, true))
			}
		}
		if turn.FinalResponse != nil {
			if body, err := json.Marshal(turn.FinalResponse); err == nil {
				recordResponsesBodyEvidence(result, body)
			}
			if responseStatusFromTurn(turn) == "completed" {
				ensureEvidence(result).CanonicalStreamTerminalSeen = true
			}
			if body, err := json.Marshal(turn.FinalResponse); err == nil && strings.TrimSpace(extractResponseText(body)) != "" {
				ensureEvidence(result).CanonicalStreamTextSeen = true
			}
		}
		if turn.ErrorEvent != nil {
			if body, err := json.Marshal(turn.ErrorEvent); err == nil {
				recordErrorEvidence(result, body)
			}
		}
		if len(turn.Errors) > 0 {
			appendStreamShapeErrors(result, turn.Errors)
		}
	}
	result.ResponseSnippet = clip(trace.String(), result.snippetLimit)
}
