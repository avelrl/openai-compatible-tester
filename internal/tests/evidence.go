package tests

import (
	"encoding/json"
	"strings"
)

type Evidence struct {
	EndpointKind                 string   `json:"endpoint_kind,omitempty"`
	ChatChoicesSeen              bool     `json:"chat_choices_seen,omitempty"`
	ResponsesOutputSeen          bool     `json:"responses_output_seen,omitempty"`
	ChatToolCallsSeen            bool     `json:"chat_tool_calls_seen,omitempty"`
	ResponsesFunctionCallSeen    bool     `json:"responses_function_call_seen,omitempty"`
	CanonicalTextSeen            bool     `json:"canonical_text_seen,omitempty"`
	CanonicalStructuredSeen      bool     `json:"canonical_structured_seen,omitempty"`
	CanonicalToolCallSeen        bool     `json:"canonical_tool_call_seen,omitempty"`
	CanonicalCustomToolCallSeen  bool     `json:"canonical_custom_tool_call_seen,omitempty"`
	CanonicalStreamTextSeen      bool     `json:"canonical_stream_text_seen,omitempty"`
	CanonicalStreamToolCallSeen  bool     `json:"canonical_stream_tool_call_seen,omitempty"`
	FallbackChatShapeOnResponses bool     `json:"fallback_chat_shape_on_responses,omitempty"`
	FallbackChatToolCallOnResp   bool     `json:"fallback_chat_tool_call_on_responses,omitempty"`
	FallbackStreamTextSeen       bool     `json:"fallback_stream_text_seen,omitempty"`
	ErrorObjectSeen              bool     `json:"error_object_seen,omitempty"`
	ErrorMessageSeen             bool     `json:"error_message_seen,omitempty"`
	ErrorTypeSeen                bool     `json:"error_type_seen,omitempty"`
	ErrorParamSeen               bool     `json:"error_param_seen,omitempty"`
	ErrorCodeSeen                bool     `json:"error_code_seen,omitempty"`
	StrictUnsupported            bool     `json:"strict_unsupported,omitempty"`
	StrictUnsupportedReason      string   `json:"strict_unsupported_reason,omitempty"`
	StreamEventTypes             []string `json:"stream_event_types,omitempty"`
}

func ensureEvidence(result *Result) *Evidence {
	if result == nil {
		return nil
	}
	if result.Evidence == nil {
		result.Evidence = &Evidence{}
	}
	return result.Evidence
}

func appendStreamEventType(result *Result, eventType string) {
	if result == nil || strings.TrimSpace(eventType) == "" {
		return
	}
	ev := ensureEvidence(result)
	for _, existing := range ev.StreamEventTypes {
		if existing == eventType {
			return
		}
	}
	ev.StreamEventTypes = append(ev.StreamEventTypes, eventType)
}

func recordChatBodyEvidence(result *Result, body []byte) {
	ev := ensureEvidence(result)
	ev.EndpointKind = KindChat
	recordErrorEvidence(result, body)

	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return
	}
	recordToolChoiceEvidence(result, doc)
	if choices, ok := doc["choices"].([]interface{}); ok && len(choices) > 0 {
		ev.ChatChoicesSeen = true
	}
	if strings.TrimSpace(extractChatContent(body)) != "" {
		ev.CanonicalTextSeen = true
	}
	if callID, _ := extractChatToolCall(body); strings.TrimSpace(callID) != "" {
		ev.ChatToolCallsSeen = true
		ev.CanonicalToolCallSeen = true
	}
}

func recordResponsesBodyEvidence(result *Result, body []byte) {
	ev := ensureEvidence(result)
	ev.EndpointKind = KindResponses
	recordErrorEvidence(result, body)

	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return
	}
	recordToolChoiceEvidence(result, doc)
	if out, ok := doc["output"].([]interface{}); ok && len(out) > 0 {
		ev.ResponsesOutputSeen = true
	}
	if choices, ok := doc["choices"].([]interface{}); ok && len(choices) > 0 {
		ev.ChatChoicesSeen = true
	}
	if strings.TrimSpace(extractCanonicalResponseText(body)) != "" {
		ev.CanonicalTextSeen = true
	}
	if callID, _ := extractCanonicalResponseFunctionCall(body); strings.TrimSpace(callID) != "" {
		ev.ResponsesFunctionCallSeen = true
		ev.CanonicalToolCallSeen = true
	}
	if callID, _, _ := extractCanonicalResponseCustomToolCall(body); strings.TrimSpace(callID) != "" {
		ev.CanonicalCustomToolCallSeen = true
	}
}

func recordErrorEvidence(result *Result, body []byte) {
	ev := ensureEvidence(result)
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return
	}
	errObj, ok := doc["error"].(map[string]interface{})
	if !ok || errObj == nil {
		return
	}
	ev.ErrorObjectSeen = true
	if s, _ := errObj["message"].(string); strings.TrimSpace(s) != "" {
		ev.ErrorMessageSeen = true
	}
	if s, _ := errObj["type"].(string); strings.TrimSpace(s) != "" {
		ev.ErrorTypeSeen = true
	}
	if v, ok := errObj["param"]; ok && strings.TrimSpace(anyToString(v)) != "" {
		ev.ErrorParamSeen = true
	}
	if v, ok := errObj["code"]; ok && strings.TrimSpace(anyToString(v)) != "" {
		ev.ErrorCodeSeen = true
	}
}

func recordToolChoiceEvidence(result *Result, doc map[string]interface{}) {
	if result == nil || doc == nil {
		return
	}
	mode := extractToolChoiceMode(doc["tool_choice"])
	if strings.TrimSpace(mode) == "" {
		return
	}
	if strings.TrimSpace(result.EffectiveToolChoice) != "" {
		return
	}
	result.EffectiveToolChoice = mode
	requested := strings.TrimSpace(result.ToolChoiceMode)
	if requested != "" && !toolChoiceMatchesRequested(requested, mode) {
		result.ToolChoiceFallback = true
	}
}

func toolChoiceMatchesRequested(requested, observed string) bool {
	requested = strings.ToLower(strings.TrimSpace(requested))
	observed = strings.ToLower(strings.TrimSpace(observed))
	if requested == "" || observed == "" {
		return true
	}
	if requested == observed {
		return true
	}
	switch requested {
	case "forced":
		return strings.HasPrefix(observed, "function")
	case "forced_compat":
		return observed == "required"
	}
	if strings.HasPrefix(requested, "function") && strings.HasPrefix(observed, "function") {
		return true
	}
	return false
}

func extractToolChoiceMode(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case map[string]interface{}:
		kind := strings.TrimSpace(firstString(t["type"]))
		switch kind {
		case "allowed_tools":
			if mode := strings.TrimSpace(firstString(t["mode"])); mode != "" {
				return kind + ":" + mode
			}
		case "function":
			if name := strings.TrimSpace(firstString(t["name"])); name != "" {
				return kind + ":" + name
			}
			if fn, ok := t["function"].(map[string]interface{}); ok {
				if name := strings.TrimSpace(firstString(fn["name"])); name != "" {
					return kind + ":" + name
				}
			}
		}
		if kind != "" {
			return kind
		}
	}
	return strings.TrimSpace(anyToString(v))
}

func extractCanonicalResponseText(body []byte) string {
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
	return ""
}

func extractCanonicalResponseFunctionCall(body []byte) (string, string) {
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", ""
	}
	out, ok := doc["output"].([]interface{})
	if !ok {
		return "", ""
	}
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
	return "", ""
}

func extractCanonicalResponseCustomToolCall(body []byte) (string, string, string) {
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
