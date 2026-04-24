package tests

import (
	"fmt"
	"strings"

	"github.com/avelrl/openai-compatible-tester/internal/config"
)

func ProjectForMode(res Result, mode string) Result {
	res = finalizeModeResult(config.ModeCompat, res)
	res = finalizeModeResult(config.ModeStrict, res)

	switch strings.TrimSpace(mode) {
	case config.ModeStrict:
		res.Status = res.SpecStatus
		res.ErrorType = res.SpecErrorType
		res.ErrorMessage = res.SpecErrorMessage
	default:
		res.Status = res.CompatStatus
		res.ErrorType = res.CompatErrorType
		res.ErrorMessage = res.CompatErrorMessage
	}
	return res
}

func finalizeModeResult(mode string, res Result) Result {
	if res.CompatStatus == "" {
		res.CompatStatus = res.Status
		res.CompatErrorType = res.ErrorType
		res.CompatErrorMessage = res.ErrorMessage
	}
	if res.SpecStatus == "" {
		res.SpecStatus, res.SpecErrorType, res.SpecErrorMessage = deriveSpecOutcome(res)
	}
	switch strings.TrimSpace(mode) {
	case config.ModeStrict:
		res.Status = res.SpecStatus
		res.ErrorType = res.SpecErrorType
		res.ErrorMessage = res.SpecErrorMessage
	default:
		res.Status = res.CompatStatus
		res.ErrorType = res.CompatErrorType
		res.ErrorMessage = res.CompatErrorMessage
	}
	return res
}

func deriveSpecOutcome(res Result) (Status, string, string) {
	compatStatus := res.CompatStatus
	if compatStatus == "" {
		compatStatus = res.Status
	}
	compatType := res.CompatErrorType
	if compatType == "" {
		compatType = res.ErrorType
	}
	compatMsg := res.CompatErrorMessage
	if compatMsg == "" {
		compatMsg = res.ErrorMessage
	}
	ev := res.Evidence

	switch compatStatus {
	case StatusTimeout:
		return compatStatus, compatType, compatMsg
	case StatusUnsupported:
		if ev != nil && ev.StrictUnsupported && hasCanonicalErrorObject(ev) {
			return compatStatus, compatType, compatMsg
		}
		if compatType == "endpoint_missing" || compatType == "unsupported_get" || IsCapabilityGateErrorType(compatType) {
			return compatStatus, compatType, compatMsg
		}
		return StatusFail, "spec_violation", "feature reported as unsupported without canonical OpenAI-style rejection"
	}

	if isExactnessOnlyFailureResult(res) && hasCanonicalBasicText(res) {
		return StatusPass, "", ""
	}

	if compatStatus != StatusPass {
		return compatStatus, compatType, compatMsg
	}

	switch res.TestID {
	case "chat.error_shape", "responses.error_shape":
		if hasCanonicalErrorObject(ev) && res.HTTPStatus >= 400 && res.HTTPStatus < 500 {
			return StatusPass, "", ""
		}
		return StatusFail, "spec_violation", "missing canonical OpenAI error object"
	case "chat.stream", "responses.stream":
		if ev != nil && ev.CanonicalStreamTextSeen && ev.CanonicalStreamTerminalSeen {
			return StatusPass, "", ""
		}
		return StatusFail, "spec_violation", "missing canonical stream text or terminal events"
	case "chat.tool_call", "chat.tool_call.required":
		if ev != nil && ev.CanonicalToolCallSeen {
			return StatusPass, "", ""
		}
		return StatusFail, "spec_violation", "missing canonical chat tool call"
	case "responses.tool_call", "responses.tool_call.required":
		if ev != nil && (ev.CanonicalToolCallSeen || ev.CanonicalStreamToolCallSeen) {
			return StatusPass, "", ""
		}
		return StatusFail, "spec_violation", "missing canonical responses function call"
	case "responses.custom_tool", "responses.custom_tool.grammar":
		if ev != nil && ev.CanonicalCustomToolCallSeen {
			return StatusPass, "", ""
		}
		return StatusFail, "spec_violation", "missing canonical responses custom tool call"
	case "chat.structured.json_schema", "chat.structured.json_object", "responses.structured.json_schema", "responses.structured.json_object":
		if ev != nil && ev.CanonicalStructuredSeen {
			return StatusPass, "", ""
		}
		return StatusFail, "spec_violation", "missing canonical structured output"
	}

	if strings.HasPrefix(res.TestID, "responses.") {
		if ev != nil && ev.CanonicalTextSeen {
			return StatusPass, "", ""
		}
		return StatusFail, "spec_violation", "missing canonical responses output shape"
	}
	if strings.HasPrefix(res.TestID, "chat.") {
		if ev == nil || ev.CanonicalTextSeen || strings.Contains(res.TestID, "memory") {
			return StatusPass, "", ""
		}
		return StatusFail, "spec_violation", "missing canonical chat output shape"
	}
	return compatStatus, compatType, compatMsg
}

func hasCanonicalBasicText(res Result) bool {
	if res.Evidence == nil {
		return false
	}
	switch res.TestID {
	case "chat.basic", "responses.basic":
		return res.Evidence.CanonicalTextSeen
	default:
		return false
	}
}

func hasCanonicalErrorObject(ev *Evidence) bool {
	return ev != nil && ev.ErrorObjectSeen && ev.ErrorMessageSeen && ev.ErrorTypeSeen
}

func isExactnessOnlyFailureResult(res Result) bool {
	return res.Status == StatusFail &&
		res.ErrorType == "assert" &&
		res.HTTPStatus >= 200 &&
		res.HTTPStatus < 300
}

func strictUnsupportedResult(result Result, reason string) Result {
	ev := ensureEvidence(&result)
	ev.StrictUnsupported = true
	ev.StrictUnsupportedReason = strings.TrimSpace(reason)
	return result
}

func strictSpecFailure(msg string) (Status, string, string) {
	return StatusFail, "spec_violation", strings.TrimSpace(msg)
}

func unexpectedSpecFailure(prefix string, res Result) (Status, string, string) {
	msg := strings.TrimSpace(res.CompatErrorMessage)
	if msg == "" {
		msg = strings.TrimSpace(res.ErrorMessage)
	}
	if msg == "" {
		msg = prefix
	} else {
		msg = fmt.Sprintf("%s: %s", prefix, msg)
	}
	return StatusFail, "spec_violation", msg
}
