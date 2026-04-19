package tests

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/avelrl/openai-compatible-tester/internal/config"
)

func capabilitiesFromDebugManifest(body []byte) (config.CapabilitiesConfig, error) {
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return config.CapabilitiesConfig{}, err
	}

	out := config.CapabilitiesConfig{Capabilities: map[string]config.CapabilitySpec{}}
	if raw, ok := doc["capabilities"]; ok {
		obj, ok := raw.(map[string]interface{})
		if !ok {
			return config.CapabilitiesConfig{}, fmt.Errorf("debug manifest capabilities is not an object")
		}
		for key, value := range obj {
			name := strings.TrimSpace(key)
			if name == "" {
				continue
			}
			spec, ok := capabilitySpecFromAny(value)
			if !ok {
				continue
			}
			out.Capabilities[name] = spec
		}
	}

	derived, derivedAny := deriveCapabilitiesFromShimManifest(doc)
	out = mergeCapabilities(out, derived)
	if len(out.Capabilities) == 0 && !derivedAny {
		return config.CapabilitiesConfig{}, fmt.Errorf("debug manifest missing capability sections")
	}
	return out, nil
}

func deriveCapabilitiesFromShimManifest(doc map[string]interface{}) (config.CapabilitiesConfig, bool) {
	out := config.CapabilitiesConfig{Capabilities: map[string]config.CapabilitySpec{}}
	any := false

	surfaces := childMap(doc, "surfaces")
	runtime := childMap(doc, "runtime")
	tools := childMap(doc, "tools")
	probes := childMap(doc, "probes")
	if surfaces != nil || runtime != nil || tools != nil || probes != nil {
		any = true
	}

	if responses := childMap(surfaces, "responses"); responses != nil {
		if spec, ok := boolToCapabilitySpec(responses, "stateful", "responses.stateful"); ok {
			out.Capabilities["responses.store"] = spec
		}
		if spec, ok := boolToCapabilitySpec(responses, "input_items", "responses.input_items"); ok {
			out.Capabilities["responses.input_items"] = spec
		}
		if spec, ok := boolToCapabilitySpec(responses, "retrieve_stream", "responses.retrieve_stream"); ok {
			out.Capabilities["responses.retrieve_stream"] = spec
		}
	}

	if conversations := childMap(surfaces, "conversations"); conversations != nil {
		if spec, ok := boolToCapabilitySpec(conversations, "items", "conversations.items"); ok {
			out.Capabilities["conversations.items"] = spec
		}
	}

	if chat := childMap(surfaces, "chat_completions"); chat != nil {
		if spec, ok := boolToCapabilitySpec(chat, "stored", "chat_completions.stored"); ok {
			out.Capabilities["chat.store"] = spec
		}
	}

	if persistence := childMap(runtime, "persistence"); persistence != nil {
		if spec, ok := boolToCapabilitySpec(persistence, "expected_durable", "runtime.persistence.expected_durable"); ok {
			out.Capabilities["persistence.restart_safe"] = spec
		}
	}

	if vectorStores := childMap(surfaces, "vector_stores"); vectorStores != nil {
		spec, ok := surfaceWithProbeCapabilitySpec(vectorStores, childMap(probes, "retrieval_embedder"), "vector_stores.enabled", "retrieval_embedder")
		if ok {
			out.Capabilities["retrieval.vector_store"] = spec
		}
	}

	if fileSearch := childMap(tools, "file_search"); fileSearch != nil {
		spec, ok := localToolCapabilitySpec(fileSearch, childMap(probes, "retrieval_embedder"), "file_search", "retrieval_embedder")
		if ok {
			out.Capabilities["tool.file_search.local"] = spec
		}
	}
	if webSearch := childMap(tools, "web_search"); webSearch != nil {
		spec, ok := localToolCapabilitySpec(webSearch, childMap(probes, "web_search_backend"), "web_search", "web_search_backend")
		if ok {
			out.Capabilities["tool.web_search.local"] = spec
		}
	}
	if imageGeneration := childMap(tools, "image_generation"); imageGeneration != nil {
		spec, ok := localToolCapabilitySpec(imageGeneration, childMap(probes, "image_generation_backend"), "image_generation", "image_generation_backend")
		if ok {
			out.Capabilities["tool.image_generation.local"] = spec
		}
	}
	if computer := childMap(tools, "computer"); computer != nil {
		if spec, ok := localToolCapabilitySpec(computer, nil, "computer", ""); ok {
			out.Capabilities["tool.computer.local"] = spec
		}
	}
	if codeInterpreter := childMap(tools, "code_interpreter"); codeInterpreter != nil {
		if spec, ok := localToolCapabilitySpec(codeInterpreter, nil, "code_interpreter", ""); ok {
			out.Capabilities["tool.code_interpreter.local"] = spec
		}
	}
	if mcpServerURL := childMap(tools, "mcp_server_url"); mcpServerURL != nil {
		if spec, ok := generalToolCapabilitySpec(mcpServerURL, "mcp_server_url"); ok {
			out.Capabilities["tool.mcp.server_url"] = spec
		}
	}
	if toolSearchHosted := childMap(tools, "tool_search_hosted"); toolSearchHosted != nil {
		if spec, ok := generalToolCapabilitySpec(toolSearchHosted, "tool_search_hosted"); ok {
			out.Capabilities["tool.tool_search.server"] = spec
		}
	}

	return out, any
}

func mergeCapabilities(base, override config.CapabilitiesConfig) config.CapabilitiesConfig {
	out := config.CapabilitiesConfig{Capabilities: map[string]config.CapabilitySpec{}}
	for key, value := range base.Capabilities {
		out.Capabilities[key] = value
	}
	for key, value := range override.Capabilities {
		out.Capabilities[key] = value
	}
	return out
}

func capabilitySpecFromAny(value interface{}) (config.CapabilitySpec, bool) {
	switch v := value.(type) {
	case string:
		return config.CapabilitySpec{Status: strings.TrimSpace(v)}, true
	case bool:
		if v {
			return config.CapabilitySpec{Status: config.CapabilityStatusSupported}, true
		}
		return config.CapabilitySpec{Status: config.CapabilityStatusUnsupported}, true
	case map[string]interface{}:
		spec := config.CapabilitySpec{
			Status: strings.TrimSpace(firstCapabilityString(v["status"])),
			Reason: firstCapabilityReason(v),
		}
		if spec.Status == "" {
			spec.Status = deriveGenericCapabilityStatus(v)
		}
		if spec.Status == "" {
			spec.Status = config.CapabilityStatusSupported
		}
		return spec, true
	default:
		return config.CapabilitySpec{}, false
	}
}

func boolToCapabilitySpec(parent map[string]interface{}, key, reasonField string) (config.CapabilitySpec, bool) {
	if parent == nil {
		return config.CapabilitySpec{}, false
	}
	value, ok := capabilityBool(parent[key])
	if !ok {
		return config.CapabilitySpec{}, false
	}
	if value {
		return config.CapabilitySpec{Status: config.CapabilityStatusSupported}, true
	}
	return config.CapabilitySpec{
		Status: config.CapabilityStatusDisabled,
		Reason: reasonField + " is false",
	}, true
}

func localToolCapabilitySpec(tool map[string]interface{}, probe map[string]interface{}, toolName, probeName string) (config.CapabilitySpec, bool) {
	if tool == nil {
		return config.CapabilitySpec{}, false
	}
	support := normalizeSupport(firstCapabilityString(tool["support"]))
	switch {
	case support == "" || support == "supported" || support == "local" || strings.Contains(support, "local"):
	case support == "unsupported" || support == "none":
		return config.CapabilitySpec{Status: config.CapabilityStatusUnsupported, Reason: toolName + " support=" + support}, true
	case strings.Contains(support, "proxy") || strings.Contains(support, "hosted") || strings.Contains(support, "connector") || strings.Contains(support, "client"):
		return config.CapabilitySpec{Status: config.CapabilityStatusUnsupported, Reason: toolName + " is not locally owned in support=" + support}, true
	}
	enabled, ok := capabilityBool(tool["enabled"])
	if ok && !enabled {
		return config.CapabilitySpec{Status: config.CapabilityStatusDisabled, Reason: toolName + " enabled=false"}, true
	}
	if probeSpec, ok := probeCapabilitySpec(probe, probeName); ok {
		return probeSpec, true
	}
	return config.CapabilitySpec{Status: config.CapabilityStatusSupported}, true
}

func generalToolCapabilitySpec(tool map[string]interface{}, toolName string) (config.CapabilitySpec, bool) {
	if tool == nil {
		return config.CapabilitySpec{}, false
	}
	support := normalizeSupport(firstCapabilityString(tool["support"]))
	if support == "unsupported" || support == "none" {
		return config.CapabilitySpec{Status: config.CapabilityStatusUnsupported, Reason: toolName + " support=" + support}, true
	}
	if enabled, ok := capabilityBool(tool["enabled"]); ok && !enabled {
		return config.CapabilitySpec{Status: config.CapabilityStatusDisabled, Reason: toolName + " enabled=false"}, true
	}
	return config.CapabilitySpec{Status: config.CapabilityStatusSupported}, true
}

func surfaceWithProbeCapabilitySpec(surface map[string]interface{}, probe map[string]interface{}, surfaceField, probeName string) (config.CapabilitySpec, bool) {
	if surface == nil {
		return config.CapabilitySpec{}, false
	}
	enabled, ok := capabilityBool(surface["enabled"])
	if ok && !enabled {
		return config.CapabilitySpec{Status: config.CapabilityStatusDisabled, Reason: surfaceField + " is false"}, true
	}
	if probeSpec, ok := probeCapabilitySpec(probe, probeName); ok {
		return probeSpec, true
	}
	return config.CapabilitySpec{Status: config.CapabilityStatusSupported}, true
}

func probeCapabilitySpec(probe map[string]interface{}, probeName string) (config.CapabilitySpec, bool) {
	if probe == nil {
		return config.CapabilitySpec{}, false
	}
	if enabled, ok := capabilityBool(probe["enabled"]); ok && !enabled {
		return config.CapabilitySpec{Status: config.CapabilityStatusDisabled, Reason: probeName + " probe is disabled"}, true
	}
	if checked, ok := capabilityBool(probe["checked"]); ok && !checked {
		return config.CapabilitySpec{Status: config.CapabilityStatusUnavailable, Reason: probeName + " probe has not completed"}, true
	}
	if ready, ok := capabilityBool(probe["ready"]); ok && !ready {
		return config.CapabilitySpec{Status: config.CapabilityStatusUnavailable, Reason: probeName + " probe is not ready"}, true
	}
	return config.CapabilitySpec{Status: config.CapabilityStatusSupported}, true
}

func deriveGenericCapabilityStatus(v map[string]interface{}) string {
	if b, ok := capabilityBool(v["supported"]); ok && !b {
		return config.CapabilityStatusUnsupported
	}
	if b, ok := capabilityBool(v["enabled"]); ok && !b {
		return config.CapabilityStatusDisabled
	}
	if b, ok := capabilityBool(v["configured"]); ok && !b {
		return config.CapabilityStatusDisabled
	}
	if b, ok := capabilityBool(v["healthy"]); ok && !b {
		return config.CapabilityStatusUnavailable
	}
	if b, ok := capabilityBool(v["reachable"]); ok && !b {
		return config.CapabilityStatusUnavailable
	}
	if b, ok := capabilityBool(v["available"]); ok && !b {
		return config.CapabilityStatusUnavailable
	}
	return ""
}

func firstCapabilityReason(v map[string]interface{}) string {
	for _, key := range []string{"reason", "message", "error"} {
		if s := strings.TrimSpace(firstCapabilityString(v[key])); s != "" {
			return s
		}
	}
	return ""
}

func childMap(parent map[string]interface{}, key string) map[string]interface{} {
	if parent == nil {
		return nil
	}
	child, _ := parent[key].(map[string]interface{})
	return child
}

func normalizeSupport(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "yes", "true", "local_first":
		return "supported"
	default:
		return value
	}
}

func capabilityBool(v interface{}) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}

func firstCapabilityString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}
