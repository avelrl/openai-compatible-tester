package tests

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/avelrl/openai-compatible-tester/internal/config"
	"github.com/avelrl/openai-compatible-tester/internal/httpclient"
)

type EventType string

const (
	EventStart  EventType = "start"
	EventFinish EventType = "finish"
	EventInfo   EventType = "info"
)

type Event struct {
	Type   EventType
	Result Result
}

type Runner struct {
	cfg       config.Config
	client    *httpclient.Client
	tests     []TestCase
	pauseMu   sync.Mutex
	pauseCond *sync.Cond
	paused    bool
}

func NewRunner(cfg config.Config, client *httpclient.Client, tests []TestCase) *Runner {
	r := &Runner{cfg: cfg, client: client, tests: tests}
	r.pauseCond = sync.NewCond(&r.pauseMu)
	return r
}

func (r *Runner) Pause() {
	r.pauseMu.Lock()
	r.paused = true
	r.pauseMu.Unlock()
}

func (r *Runner) Resume() {
	r.pauseMu.Lock()
	r.paused = false
	r.pauseCond.Broadcast()
	r.pauseMu.Unlock()
}

func (r *Runner) IsPaused() bool {
	r.pauseMu.Lock()
	defer r.pauseMu.Unlock()
	return r.paused
}

func (r *Runner) waitIfPaused(ctx context.Context) error {
	r.pauseMu.Lock()
	defer r.pauseMu.Unlock()
	for r.paused {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r.pauseCond.Wait()
	}
	return nil
}

func (r *Runner) Run(ctx context.Context, profiles []config.ModelProfile, onEvent func(Event)) ([]Result, error) {
	limit := r.cfg.Suite.Report.SnippetLimitBytes
	if limit <= 0 {
		limit = 4096
	}
	setSnippetLimit(limit)
	tests := r.filterTests()
	if len(tests) == 0 {
		return nil, fmt.Errorf("no tests selected")
	}
	if len(profiles) == 0 {
		return nil, fmt.Errorf("no profiles selected")
	}

	passes := r.cfg.Suite.Passes
	warmups := r.cfg.Suite.WarmupPasses
	totalPasses := passes + warmups
	jobs := make(chan job)
	results := make([]Result, 0)
	var resultsMu sync.Mutex

	workerCount := r.cfg.Suite.Parallelism
	if workerCount < 1 {
		workerCount = 1
	}
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				skipRest := false
				for _, test := range tests {
					if ctx.Err() != nil {
						return
					}
					if err := r.waitIfPaused(ctx); err != nil {
						return
					}
					startRes := baseRunResult(test, j, StatusRunning)
					if onEvent != nil {
						onEvent(Event{Type: EventStart, Result: startRes})
					}
					if skipRest {
						res := skippedResult(test, j, "sanity_failed", "sanity failed; skipping remaining tests")
						resultsMu.Lock()
						results = append(results, res)
						resultsMu.Unlock()
						if onEvent != nil {
							onEvent(Event{Type: EventFinish, Result: res})
						}
						continue
					}
					res := r.runTestWithRetries(ctx, test, j)
					resultsMu.Lock()
					results = append(results, res)
					resultsMu.Unlock()
					if onEvent != nil {
						onEvent(Event{Type: EventFinish, Result: res})
					}
					if test.Kind == KindSanity && (res.Status == StatusFail || res.Status == StatusUnsupported) {
						skipRest = true
					}
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for pass := 1; pass <= totalPasses; pass++ {
			isWarmup := pass <= warmups
			for _, profile := range profiles {
				select {
				case jobs <- job{Profile: profile, Pass: pass, IsWarmup: isWarmup}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	wg.Wait()
	return results, nil
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func (r *Runner) runTestWithRetries(ctx context.Context, test TestCase, job job) Result {
	maxAttempts := r.cfg.Suite.Retry.TestRetries + 1
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var res Result
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		timeoutSeconds := r.cfg.Suite.TimeoutSeconds
		if ov, ok := testOverrideForProfile(r.cfg, job.Profile, test.ID); ok && ov.TimeoutSeconds > 0 {
			timeoutSeconds = ov.TimeoutSeconds
		}
		runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
		res = r.safeRunTest(runCtx, test, job)
		cancel()
		res = r.applyTimeoutPolicy(test.ID, job.Profile, res)
		res.Attempts = attempt
		if !shouldRetryTestResult(res) || attempt == maxAttempts || ctx.Err() != nil {
			return res
		}
		if !sleepWithContext(ctx, time.Duration(r.cfg.Suite.Retry.BackoffMS*attempt)*time.Millisecond) {
			return res
		}
	}
	return res
}

func shouldRetryTestResult(res Result) bool {
	return res.Status == StatusFail || res.Status == StatusTimeout
}

func (r *Runner) applyTimeoutPolicy(testID string, profile config.ModelProfile, res Result) Result {
	if res.Status != StatusTimeout {
		return res
	}
	if ov, ok := testOverrideForProfile(r.cfg, profile, testID); ok && ov.TreatTimeoutAsUnsupported {
		if len(ov.TreatTimeoutAsUnsupportedProfiles) == 0 || containsString(ov.TreatTimeoutAsUnsupportedProfiles, profile.Name) {
			res.Status = StatusUnsupported
		}
	}
	return res
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

type job struct {
	Profile  config.ModelProfile
	Pass     int
	IsWarmup bool
}

func (r *Runner) safeRunTest(ctx context.Context, test TestCase, job job) Result {
	res := Result{}
	defer func() {
		if rec := recover(); rec != nil {
			res = Result{
				TestID:       test.ID,
				TestName:     test.Name,
				Profile:      job.Profile.Name,
				Pass:         job.Pass,
				Model:        modelFor(test, job.Profile),
				Status:       StatusFail,
				IsWarmup:     job.IsWarmup,
				ErrorType:    "panic",
				ErrorMessage: fmt.Sprintf("%v", rec),
			}
		}
	}()
	res = test.Run(ctx, RunContext{
		Client:   r.client,
		Config:   r.cfg,
		Profile:  job.Profile,
		Pass:     job.Pass,
		IsWarmup: job.IsWarmup,
	})
	if res.TestID == "" {
		res.TestID = test.ID
		res.TestName = test.Name
		res.Profile = job.Profile.Name
		res.Pass = job.Pass
		res.Model = modelFor(test, job.Profile)
		res.IsWarmup = job.IsWarmup
	}
	if res.Model == "" {
		res.Model = modelFor(test, job.Profile)
	}
	if res.Status == StatusRunning || res.Status == StatusQueued {
		res.Status = StatusFail
	}
	return res
}

func baseRunResult(test TestCase, job job, status Status) Result {
	return Result{
		TestID:   test.ID,
		TestName: test.Name,
		Profile:  job.Profile.Name,
		Pass:     job.Pass,
		Model:    modelFor(test, job.Profile),
		Status:   status,
		IsWarmup: job.IsWarmup,
	}
}

func skippedResult(test TestCase, job job, errType, msg string) Result {
	res := baseRunResult(test, job, StatusUnsupported)
	res.ErrorType = errType
	res.ErrorMessage = msg
	return res
}

func modelFor(test TestCase, profile config.ModelProfile) string {
	if test.Kind == KindChat {
		return profile.ChatModel
	}
	return profile.ResponsesModel
}

func (r *Runner) filterTests() []TestCase {
	out := make([]TestCase, 0, len(r.tests))
	for _, t := range r.tests {
		if ov, ok := r.cfg.Suite.Tests[t.ID]; ok && ov.Enabled != nil && !*ov.Enabled {
			continue
		}
		if t.RequiresStream && !r.cfg.Suite.Stream.Enabled {
			continue
		}
		if t.RequiresTools && !r.cfg.Suite.ToolCalling.Enabled {
			continue
		}
		if t.RequiresStructured && !r.cfg.Suite.StructuredOutputs.Enabled {
			continue
		}
		if t.RequiresConversations && !r.cfg.Suite.Conversations.Enabled {
			continue
		}
		if t.RequiresMemory && !r.cfg.Suite.Memory.Enabled {
			continue
		}
		out = append(out, t)
	}
	return out
}
