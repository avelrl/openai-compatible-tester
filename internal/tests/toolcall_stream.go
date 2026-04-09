package tests

import (
	"encoding/json"
	"strings"
)

// parseResponsesToolCallStreamEvent extracts tool call details from a single SSE "data:" payload.
// It supports both Responses-style events (response.*) and Chat-style chunk events.
func parseResponsesToolCallStreamEvent(data string) (callID string, name string, argsPiece string) {
	callID, name, argsPiece, _, _ = parseResponsesToolCallStreamEventDetailed(data)
	return callID, name, argsPiece
}

func parseResponsesToolCallStreamEventDetailed(data string) (callID string, name string, argsPiece string, eventType string, canonical bool) {
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" {
		return "", "", "", "[DONE]", false
	}

	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(data), &doc); err != nil {
		return "", "", "", "", false
	}
	eventType, _ = doc["type"].(string)
	if item, ok := doc["item"].(map[string]interface{}); ok && item != nil {
		if t, _ := item["type"].(string); t == "function_call" {
			callID = firstString(item["call_id"], item["id"])
			name, _ = item["name"].(string)
			argsPiece = anyToString(item["arguments"])
			return callID, name, argsPiece, firstString(eventType, "response.output_item"), true
		}
	}
	if eventType == "response.function_call_arguments.delta" || eventType == "response.function_call_arguments.done" {
		callID = firstString(doc["call_id"], doc["item_id"], doc["id"])
		name, _ = doc["name"].(string)
		argsPiece = firstString(doc["delta"], doc["arguments"])
		return callID, name, argsPiece, eventType, true
	}
	if item, ok := doc["item"].(map[string]interface{}); ok && item != nil {
		if t, _ := item["type"].(string); t == "function_call" {
			callID = firstString(item["call_id"], item["id"])
			name, _ = item["name"].(string)
			argsPiece = anyToString(item["arguments"])
			return callID, name, argsPiece, firstString(eventType, "response.output_item"), true
		}
	}
	if delta, ok := doc["delta"].(map[string]interface{}); ok && delta != nil {
		// Some servers include function call details directly under delta.
		if t, _ := delta["type"].(string); t == "function_call" || delta["arguments"] != nil {
			callID = firstString(delta["call_id"], delta["id"])
			name, _ = delta["name"].(string)
			argsPiece = anyToString(delta["arguments"])
			return callID, name, argsPiece, firstString(eventType, "fallback.delta"), false
		}
		if fc, ok := delta["function_call"].(map[string]interface{}); ok && fc != nil {
			callID = firstString(fc["call_id"], fc["id"])
			name, _ = fc["name"].(string)
			argsPiece = anyToString(fc["arguments"])
			return callID, name, argsPiece, firstString(eventType, "fallback.delta.function_call"), false
		}
	}

	// Chat-style chunk events (as a fallback)
	if choices, ok := doc["choices"].([]interface{}); ok && len(choices) > 0 {
		c0, _ := choices[0].(map[string]interface{})
		if c0 != nil {
			if delta, ok := c0["delta"].(map[string]interface{}); ok && delta != nil {
				if calls, ok := delta["tool_calls"].([]interface{}); ok && len(calls) > 0 {
					call, _ := calls[0].(map[string]interface{})
					if call != nil {
						callID = firstString(call["id"])
						if fn, ok := call["function"].(map[string]interface{}); ok && fn != nil {
							name, _ = fn["name"].(string)
							if a, ok := fn["arguments"].(string); ok {
								argsPiece = a
							}
						}
						return callID, name, argsPiece, firstString(eventType, "chat.completions.chunk"), false
					}
				}
			}
		}
	}

	return "", "", "", eventType, false
}

func mergeArgsJSON(cur, piece string) string {
	if strings.TrimSpace(piece) == "" {
		return cur
	}
	if cur == "" {
		return piece
	}
	if strings.HasPrefix(piece, cur) {
		// Some implementations send the full arguments string repeatedly; treat it as replacement.
		return piece
	}
	if strings.HasPrefix(cur, piece) {
		// Repeated prefix.
		return cur
	}
	return cur + piece
}

func isValidJSON(s string) bool {
	var v interface{}
	return json.Unmarshal([]byte(strings.TrimSpace(s)), &v) == nil
}

func firstString(vals ...interface{}) string {
	for _, v := range vals {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func anyToString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(b)
	}
}
