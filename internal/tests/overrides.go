package tests

import (
	"strings"

	"github.com/avelrl/openai-compatible-tester/internal/config"
)

func testOverride(cfg config.Config, testID string) (config.TestOverride, bool) {
	ov, ok := cfg.Suite.Tests[testID]
	if ok {
		return ov, true
	}
	if base := baseTestID(testID); base != testID {
		if ov, ok := cfg.Suite.Tests[base]; ok {
			return ov, true
		}
	}
	return config.TestOverride{}, false
}

func baseTestID(testID string) string {
	for _, suffix := range []string{".required", ".auto"} {
		if strings.HasSuffix(testID, suffix) {
			return strings.TrimSuffix(testID, suffix)
		}
	}
	return testID
}

func requestHeadersForTest(cfg config.Config, testID string) map[string]string {
	merged := map[string]string{}
	for k, v := range cfg.Suite.LiteLLMHeaders {
		merged[k] = v
	}
	if ov, ok := testOverride(cfg, testID); ok {
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

func effectiveHeaderValueForTest(cfg config.Config, testID, key string) string {
	if ov, ok := testOverride(cfg, testID); ok {
		if v := headerValue(ov.LiteLLMHeaders, key); v != "" {
			return v
		}
	}
	if v := headerValue(cfg.Suite.LiteLLMHeaders, key); v != "" {
		return v
	}
	return headerValue(cfg.Endpoints.DefaultHeaders, key)
}
