package tui

import (
	"testing"

	"github.com/avelrl/openai-compatible-tester/internal/config"
	"github.com/avelrl/openai-compatible-tester/internal/tests"
)

func TestFinalizeRunStateReplacesLiveResults(t *testing.T) {
	ui := &UI{
		results: []tests.Result{
			{TestID: "chat.basic", Profile: "p1", Pass: 1, Status: tests.StatusPass},
			{TestID: "chat.structured.json_object", Profile: "p1", Pass: 1, Status: tests.StatusPass},
			{TestID: "chat.structured.json_object", Profile: "p1", Pass: 1, Status: tests.StatusPass},
		},
		finalResults: []tests.Result{
			{TestID: "chat.basic", Profile: "p1", Pass: 1, Status: tests.StatusPass},
			{TestID: "chat.structured.json_object", Profile: "p1", Pass: 1, Status: tests.StatusPass},
		},
		statuses: map[string]tests.Status{},
	}

	ui.finalizeRunState()

	if len(ui.results) != 2 {
		t.Fatalf("expected final results to replace live duplicates, got %d", len(ui.results))
	}
	if ui.doneJobs != 2 {
		t.Fatalf("expected doneJobs=2, got %d", ui.doneJobs)
	}
	if got := ui.statuses[statusKey("chat.structured.json_object", "p1", 1)]; got != tests.StatusPass {
		t.Fatalf("unexpected status %q", got)
	}
}

func TestHandleTestEventDoesNotAppendAfterCompletion(t *testing.T) {
	ui := &UI{
		runComplete: true,
		statuses:    map[string]tests.Status{},
		results: []tests.Result{
			{TestID: "chat.basic", Profile: "p1", Pass: 1, Status: tests.StatusPass},
		},
	}

	ui.handleTestEvent(tests.Event{
		Type: tests.EventFinish,
		Result: tests.Result{
			TestID:  "chat.basic",
			Profile: "p1",
			Pass:    1,
			Status:  tests.StatusPass,
		},
	})

	if len(ui.results) != 1 {
		t.Fatalf("expected no append after completion, got %d results", len(ui.results))
	}
}

func TestStopRunResumesPausedRunner(t *testing.T) {
	runner := tests.NewRunner(config.Config{}, nil, nil)
	runner.Pause()
	called := false
	ui := &UI{
		runner: runner,
		runCancel: func() {
			called = true
		},
	}

	ui.stopRun()

	if runner.IsPaused() {
		t.Fatal("expected paused runner to be resumed")
	}
	if !called {
		t.Fatal("expected cancel func to be called")
	}
}

func TestStopRunHandlesNilCancel(t *testing.T) {
	runner := tests.NewRunner(config.Config{}, nil, nil)
	runner.Pause()
	ui := &UI{runner: runner}

	ui.stopRun()

	if runner.IsPaused() {
		t.Fatal("expected paused runner to be resumed")
	}
}
