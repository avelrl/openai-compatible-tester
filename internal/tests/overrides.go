package tests

import (
	"strings"

	"github.com/avelrl/openai-compatible-tester/internal/config"
)

func resolveTestOverride(overrides map[string]config.TestOverride, testID string) (config.TestOverride, bool) {
	if len(overrides) == 0 {
		return config.TestOverride{}, false
	}
	if ov, ok := overrides[testID]; ok {
		return ov, true
	}
	if base := baseTestID(testID); base != testID {
		if ov, ok := overrides[base]; ok {
			return ov, true
		}
	}
	return config.TestOverride{}, false
}

func mergeTestOverride(base, overlay config.TestOverride) config.TestOverride {
	merged := base
	if overlay.Enabled != nil {
		merged.Enabled = overlay.Enabled
	}
	if overlay.TimeoutSeconds > 0 {
		merged.TimeoutSeconds = overlay.TimeoutSeconds
	}
	if overlay.Stream != nil {
		merged.Stream = overlay.Stream
	}
	if overlay.StreamTimeoutSeconds > 0 {
		merged.StreamTimeoutSeconds = overlay.StreamTimeoutSeconds
	}
	if len(base.LiteLLMHeaders) > 0 || len(overlay.LiteLLMHeaders) > 0 {
		merged.LiteLLMHeaders = cloneHeaders(base.LiteLLMHeaders)
		for k, v := range overlay.LiteLLMHeaders {
			merged.LiteLLMHeaders[k] = v
		}
	}
	if v := strings.TrimSpace(overlay.InstructionRole); v != "" {
		merged.InstructionRole = v
	}
	if v := strings.TrimSpace(overlay.ToolChoiceMode); v != "" {
		merged.ToolChoiceMode = v
	}
	if v := strings.TrimSpace(overlay.ForcedToolName); v != "" {
		merged.ForcedToolName = v
	}
	if overlay.ParallelToolCalls != nil {
		merged.ParallelToolCalls = overlay.ParallelToolCalls
	}
	if v := strings.TrimSpace(overlay.ReasoningEffort); v != "" {
		merged.ReasoningEffort = v
	}
	if overlay.MaxOutputTokens != nil {
		merged.MaxOutputTokens = overlay.MaxOutputTokens
	}
	if overlay.MaxTokens != nil {
		merged.MaxTokens = overlay.MaxTokens
	}
	if overlay.StrictMode != nil {
		merged.StrictMode = overlay.StrictMode
	}
	if overlay.TreatTimeoutAsUnsupported {
		merged.TreatTimeoutAsUnsupported = true
	}
	if len(overlay.TreatTimeoutAsUnsupportedProfiles) > 0 {
		merged.TreatTimeoutAsUnsupportedProfiles = append([]string(nil), overlay.TreatTimeoutAsUnsupportedProfiles...)
	}
	return merged
}

func testOverrideForProfile(cfg config.Config, profile config.ModelProfile, testID string) (config.TestOverride, bool) {
	suiteOV, suiteOK := resolveTestOverride(cfg.Suite.Tests, testID)
	profileOV, profileOK := resolveTestOverride(profile.Tests, testID)
	switch {
	case suiteOK && profileOK:
		return mergeTestOverride(suiteOV, profileOV), true
	case profileOK:
		return profileOV, true
	case suiteOK:
		return suiteOV, true
	default:
		return config.TestOverride{}, false
	}
}

func testOverride(cfg config.Config, testID string) (config.TestOverride, bool) {
	return testOverrideForProfile(cfg, config.ModelProfile{}, testID)
}

func baseTestID(testID string) string {
	for _, suffix := range []string{".required", ".auto"} {
		if strings.HasSuffix(testID, suffix) {
			return strings.TrimSuffix(testID, suffix)
		}
	}
	return testID
}

func requestHeadersForTest(cfg config.Config, profile config.ModelProfile, testID string) map[string]string {
	merged := map[string]string{}
	for k, v := range cfg.Suite.LiteLLMHeaders {
		merged[k] = v
	}
	if ov, ok := testOverrideForProfile(cfg, profile, testID); ok {
		for k, v := range ov.LiteLLMHeaders {
			merged[k] = v
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func headerValue(headers map[string]string, key string) string {
	if headers == nil {
		return ""
	}
	for k, v := range headers {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

func cloneHeaders(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func effectiveHeaderValueForTest(cfg config.Config, profile config.ModelProfile, testID, key string) string {
	if ov, ok := testOverrideForProfile(cfg, profile, testID); ok {
		if v := headerValue(ov.LiteLLMHeaders, key); v != "" {
			return v
		}
	}
	if v := headerValue(cfg.Suite.LiteLLMHeaders, key); v != "" {
		return v
	}
	return headerValue(cfg.Endpoints.DefaultHeaders, key)
}
