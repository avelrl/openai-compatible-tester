package sse

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	input := "data: first\n\n" +
		"event: message\n" +
		"data: line1\n" +
		"data: line2\n\n"
	var events []Event
	err := Parse(strings.NewReader(input), func(ev Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Data != "first" {
		t.Fatalf("event0 data=%q", events[0].Data)
	}
	if events[1].Event != "message" {
		t.Fatalf("event1 event=%q", events[1].Event)
	}
	if events[1].Data != "line1\nline2" {
		t.Fatalf("event1 data=%q", events[1].Data)
	}
}
