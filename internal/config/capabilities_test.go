package config

import "testing"

func TestValidateCapabilitiesRejectsUnknownStatus(t *testing.T) {
	err := validateCapabilities(CapabilitiesConfig{
		Capabilities: map[string]CapabilitySpec{
			"tool.web_search.local": {Status: "maybe"},
		},
	})
	if err == nil {
		t.Fatal("expected error for unknown capability status")
	}
}

func TestCapabilitiesLookupNormalizesStatusAndReason(t *testing.T) {
	cfg := CapabilitiesConfig{
		Capabilities: map[string]CapabilitySpec{
			"tool.web_search.local": {
				Status: " Disabled ",
				Reason: "  backend is off  ",
			},
		},
	}
	spec, ok := cfg.Lookup("tool.web_search.local")
	if !ok {
		t.Fatal("expected capability lookup to succeed")
	}
	if spec.Status != CapabilityStatusDisabled {
		t.Fatalf("unexpected normalized status: %q", spec.Status)
	}
	if spec.Reason != "backend is off" {
		t.Fatalf("unexpected normalized reason: %q", spec.Reason)
	}
}
