package tests

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	capabilities config.CapabilitiesConfig
	pauseMu   sync.Mutex
	pauseCond *sync.Cond
	paused    bool
}

func NewRunner(cfg config.Config, client *httpclient.Client, tests []TestCase) *Runner {
	r := &Runner{cfg: cfg, client: client, tests: tests, capabilities: cfg.Capabilities}
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
	r.prepareCapabilities(ctx)

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
	if res, blocked := r.capabilityGateResult(test, job); blocked {
		return finalizeModeResult(r.cfg.Suite.Mode, res)
	}
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
		attemptCtx := ctx
		if job.Profile.RateLimitPerMinute > 0 {
			attemptCtx = httpclient.WithRateLimit(attemptCtx, job.Profile.Name, job.Profile.RateLimitPerMinute)
			if r.client != nil {
				reservedCtx, err := r.client.ReserveRateLimit(attemptCtx)
				if err != nil {
					return rateLimitReservationErrorResult(test, job, err)
				}
				attemptCtx = reservedCtx
			}
		}
		runCtx, cancel := context.WithTimeout(attemptCtx, time.Duration(timeoutSeconds)*time.Second)
		res = r.safeRunTest(runCtx, test, job)
		cancel()
		res = r.applyTimeoutPolicy(test.ID, job.Profile, res)
		res = finalizeModeResult(r.cfg.Suite.Mode, res)
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

func (r *Runner) capabilityGateResult(test TestCase, job job) (Result, bool) {
	if len(test.RequiredCapabilities) == 0 {
		return Result{}, false
	}
	if len(r.capabilities.Capabilities) == 0 {
		return capabilityBlockedResult(
			test,
			job,
			ErrorTypeCapabilityDisabled,
			fmt.Sprintf("no capability manifest configured; required capabilities: %s", strings.Join(test.RequiredCapabilities, ", ")),
		), true
	}
	for _, name := range test.RequiredCapabilities {
		spec, ok := r.capabilities.Lookup(name)
		if !ok {
			return capabilityBlockedResult(
				test,
				job,
				ErrorTypeCapabilityDisabled,
				fmt.Sprintf("required capability %s is not declared in the capability manifest", name),
			), true
		}
		switch spec.Status {
		case config.CapabilityStatusSupported:
			continue
		case config.CapabilityStatusUnsupported:
			return capabilityBlockedResult(test, job, ErrorTypeCapabilityUnsupported, capabilityMessage(name, "is not supported by this target", spec.Reason)), true
		case config.CapabilityStatusDisabled:
			return capabilityBlockedResult(test, job, ErrorTypeCapabilityDisabled, capabilityMessage(name, "is disabled in this environment", spec.Reason)), true
		case config.CapabilityStatusUnavailable:
			return capabilityBlockedResult(test, job, ErrorTypeDependencyUnavailable, capabilityMessage(name, "is currently unavailable because an external dependency is missing or unhealthy", spec.Reason)), true
		default:
			return capabilityBlockedResult(test, job, ErrorTypeCapabilityDisabled, capabilityMessage(name, "has an invalid manifest status", spec.Status)), true
		}
	}
	return Result{}, false
}

func (r *Runner) prepareCapabilities(ctx context.Context) {
	r.capabilities = r.cfg.Capabilities
	if strings.TrimSpace(r.cfg.Suite.Target) != "llama_shim" {
		return
	}
	if r.client == nil {
		return
	}
	path := strings.TrimSpace(r.cfg.Endpoints.Paths.DebugCapabilities)
	if path == "" {
		return
	}
	resp, err := r.client.Get(ctx, path, nil)
	if err != nil || resp == nil {
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return
	}
	probed, err := capabilitiesFromDebugManifest(resp.Body)
	if err != nil {
		return
	}
	r.capabilities = mergeCapabilities(r.capabilities, probed)
}

func capabilityBlockedResult(test TestCase, job job, errorType, msg string) Result {
	res := baseRunResult(test, job, StatusUnsupported)
	res.ErrorType = errorType
	res.ErrorMessage = msg
	return res
}

func capabilityMessage(name, summary, reason string) string {
	msg := fmt.Sprintf("required capability %s %s", name, summary)
	if reason = strings.TrimSpace(reason); reason != "" {
		msg += ": " + reason
	}
	return msg
}

func rateLimitReservationErrorResult(test TestCase, job job, err error) Result {
	res := Result{
		TestID:   test.ID,
		TestName: test.Name,
		Profile:  job.Profile.Name,
		Pass:     job.Pass,
		Model:    job.Profile.ResponsesModel,
		IsWarmup: job.IsWarmup,
	}
	if errors.Is(err, context.DeadlineExceeded) {
		res.Status = StatusTimeout
		res.ErrorType = "timeout"
		res.ErrorMessage = err.Error()
		return res
	}
	res.Status = StatusFail
	res.ErrorType = "http_error"
	res.ErrorMessage = err.Error()
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
		if !t.AppliesToTarget(r.cfg.Suite.Target) {
			continue
		}
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
