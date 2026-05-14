package tests

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

func validateResponsesStreamEventShape(data string, websocketTransport bool) []string {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" || trimmed == "[DONE]" {
		return nil
	}
	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &doc); err != nil {
		return []string{fmt.Sprintf("event data is not valid JSON: %v", err)}
	}
	return validateResponsesStreamEventDoc(doc, websocketTransport)
}

func validateResponsesStreamEventDoc(doc map[string]interface{}, websocketTransport bool) []string {
	if doc == nil {
		return []string{"event data is not an object"}
	}
	eventType, _ := doc["type"].(string)
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		if _, ok := doc["choices"].([]interface{}); ok {
			return nil
		}
		return []string{"event is missing string type"}
	}

	errs := make([]string, 0, 4)
	add := func(format string, args ...interface{}) {
		errs = append(errs, fmt.Sprintf(format, args...))
	}
	requireSeq := func() {
		if !isJSONInteger(doc["sequence_number"]) {
			add("%s missing integer sequence_number", eventType)
		}
	}
	requireString := func(field string) {
		if strings.TrimSpace(firstString(doc[field])) == "" {
			add("%s missing string %s", eventType, field)
		}
	}
	requireInteger := func(field string) {
		if !isJSONInteger(doc[field]) {
			add("%s missing integer %s", eventType, field)
		}
	}
	requireObject := func(field string) map[string]interface{} {
		m, _ := doc[field].(map[string]interface{})
		if m == nil {
			add("%s missing object %s", eventType, field)
		}
		return m
	}

	switch eventType {
	case "error":
		if websocketTransport {
			requireInteger("status")
			errObj := requireObject("error")
			if errObj == nil {
				break
			}
			if strings.TrimSpace(firstString(errObj["message"])) == "" {
				add("%s missing error.message", eventType)
			}
			if strings.TrimSpace(firstString(errObj["code"])) == "" && strings.TrimSpace(firstString(errObj["type"])) == "" {
				add("%s missing error.code or error.type", eventType)
			}
			break
		}
		requireSeq()
		requireString("code")
		requireString("message")
		if _, ok := doc["param"]; !ok {
			add("%s missing param", eventType)
		}
	case "response.created", "response.queued", "response.in_progress", "response.completed", "response.failed", "response.incomplete":
		requireSeq()
		resp := requireObject("response")
		if resp != nil {
			if strings.TrimSpace(firstString(resp["id"])) == "" {
				add("%s response missing id", eventType)
			}
			if strings.TrimSpace(firstString(resp["object"])) == "" {
				add("%s response missing object", eventType)
			}
			if strings.TrimSpace(firstString(resp["status"])) == "" {
				add("%s response missing status", eventType)
			}
		}
	case "response.output_item.added", "response.output_item.done":
		requireSeq()
		requireInteger("output_index")
		if _, ok := doc["item"]; !ok {
			add("%s missing item", eventType)
		}
	case "response.content_part.added", "response.content_part.done":
		requireSeq()
		requireString("item_id")
		requireInteger("output_index")
		requireInteger("content_index")
		part := requireObject("part")
		if part != nil && strings.TrimSpace(firstString(part["type"])) == "" {
			add("%s part missing type", eventType)
		}
	case "response.output_text.delta":
		requireSeq()
		requireString("item_id")
		requireInteger("output_index")
		requireInteger("content_index")
		requireString("delta")
	case "response.output_text.done":
		requireSeq()
		requireString("item_id")
		requireInteger("output_index")
		requireInteger("content_index")
		requireString("text")
	case "response.refusal.delta":
		requireSeq()
		requireString("item_id")
		requireInteger("output_index")
		requireInteger("content_index")
		requireString("delta")
	case "response.refusal.done":
		requireSeq()
		requireString("item_id")
		requireInteger("output_index")
		requireInteger("content_index")
		requireString("refusal")
	case "response.function_call_arguments.delta":
		requireSeq()
		requireString("item_id")
		requireInteger("output_index")
		requireString("delta")
	case "response.function_call_arguments.done":
		requireSeq()
		requireString("item_id")
		requireInteger("output_index")
		requireString("arguments")
	default:
		if strings.HasPrefix(eventType, "response.") {
			requireSeq()
		}
	}
	return errs
}

func shouldValidateResponsesStreamEventType(eventType string) bool {
	eventType = strings.TrimSpace(eventType)
	return eventType == "error" || strings.HasPrefix(eventType, "response.")
}

func isJSONInteger(v interface{}) bool {
	switch n := v.(type) {
	case int:
		return true
	case int64:
		return true
	case float64:
		return math.Mod(n, 1) == 0
	default:
		return false
	}
}
