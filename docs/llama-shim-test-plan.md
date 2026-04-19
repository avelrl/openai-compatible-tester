# `llama_shim` Targeted Test Plan

Last updated: April 15, 2026.

## Goal

Extend `openai-compatible-tester` with a focused path for validating the
practical `llama_shim` V2 surface, not just generic OpenAI-style
interoperability.

The generic suite already answers:

- does the endpoint look OpenAI-compatible?
- do basic `chat` and `responses` flows work?
- are streaming, structured outputs, and function tools usable at all?

That is necessary, but not sufficient for `llama_shim`.

`llama_shim` V2 now ships a broader local-first facade with stateful
`/v1/responses`, `/v1/conversations`, stored chat compatibility, local
retrieval, local/native tool subsets, MCP bridging, and automatic context
compaction. Those need deeper scenario coverage than the current generic smoke
tests provide.

## Recommendation

Do not introduce a third top-level verdict mode next to `compat` and `strict`
as the first step.

Start with a dedicated `llama_shim` target suite built on top of the current
architecture:

- keep `compat` and `strict` semantics unchanged
- add a shim-focused suite config and profile preset
- add shim-specific tests with explicit IDs
- optionally add a target-specific report section later if the extra signal is
  useful

Why this shape is preferable:

- the current runner already supports `suite`, `profile`, per-test overrides,
  and detailed per-test reporting
- `compat` vs `strict` answers a different question than "is this shim feature
  set working end to end?"
- a separate target layer keeps generic provider testing clean and avoids
  overfitting the main harness to one implementation

If target-specific reporting becomes important later, add a separate concept
such as `suite.target: llama_shim` or `--target llama_shim` without overloading
the existing verdict modes.

Architecturally, keep these as two different axes:

- verdict mode answers "how do we judge OpenAI-compatibility semantics?" and
  remains `compat` or `strict`
- suite or target depth answers "what do we exercise?" and can be generic smoke
  or deeper target-specific contract coverage

That separation matters because a target-specific deep suite is not a third
compatibility verdict. It is an additional scenario layer that can still run in
`compat` or, later, in `strict` where individual tests make sense.

## What The New Coverage Should Prove

The new shim-focused path should verify that `llama_shim` is functionally
useful for real agent workflows in the areas where V2 makes explicit
compatibility or usability claims:

- local-first `responses` lifecycle
- multi-turn state via `previous_response_id`
- durable thread state via `/v1/conversations`
- retrieve and replay behavior for stored responses
- local automatic compaction via `context_management.compact_threshold`
- local/native tool execution and follow-up flows
- local retrieval and `file_search`
- local stored chat compatibility

It should not claim exact hosted OpenAI choreography where the shim does not
claim it.

## Scope Boundary

This suite should validate the shim's documented contract, not invent a stronger
promise than the shim itself makes.

In particular:

- do validate final JSON shape, state transitions, typed stored items, and
  practical replay behavior
- do validate that claimed local tool subsets are usable
- do validate `prefer_local`, `prefer_upstream`, and `local_only` behavior when
  the shim documents those modes
- do not require exact hosted SSE event families for tool-specific flows unless
  the shim explicitly claims that parity
- do not turn ranking quality or reranker behavior into a hosted-parity test;
  keep it functional and contract-oriented

## Design Rules For Long-Term Growth

The plan should stay viable beyond V2.

That means the tester needs to scale in three dimensions without being
rewritten every release:

- new HTTP-visible compatibility work in V3
- extension behavior in V4 that is still observable over the public shim API
- increasing automation in CI, nightly regression, and agent-driven debugging

To keep that sustainable:

- keep the external tester focused on observable HTTP behavior and stored
  artifacts
- keep purely internal plugin and backend contracts in `llama_shim`'s own test
  suite
- use the external tester for end-to-end proof that a user-facing behavior
  actually works through the API surface

Practical rule:

- if a behavior is visible through `/v1/...`, it belongs here
- if a behavior is an internal backend contract such as `MemoryStore`,
  `RetrievalStore`, `Compactor`, `Reranker`, or plugin wiring, it should get
  internal contract tests in `llama_shim`, plus at most one or two end-to-end
  API scenarios here

## Proposed Deliverables

### 1. Dedicated shim configs

Add a dedicated config layer instead of burying shim tuning in the generic
defaults.

Suggested files:

- `configs/models_llama_shim.yaml`
- `configs/suite_llama_shim.yaml`

The suite should:

- keep `mode: compat` as the default primary mode
- enable streaming, tools, structured outputs, memory, and conversations
- use conservative timeouts for local tool flows
- explicitly enable only the shim-specific tests once they exist

The profile should:

- point both surfaces at the intended shim models
- allow profile-specific request shaping where the shim expects it
- isolate local deployment assumptions from the generic `models.yaml`

### 2. Shim-specific scenario tests

Add dedicated tests instead of overloading the existing generic checks with too
many implementation-specific assertions.

Suggested implementation files:

- `internal/tests/shim_responses_state.go`
- `internal/tests/shim_responses_compaction.go`
- `internal/tests/shim_tools.go`
- `internal/tests/shim_chat_store.go`
- `internal/tests/shim_retrieval.go`

The generic registry can continue to hold the test declarations and IDs.

### 3. Targeted reporting

The initial version can reuse the current reporting pipeline.

Optional follow-up if the suite becomes a first-class path:

- add a dedicated "llama_shim readiness" section in `summary.md`
- group failures by feature family such as state, compaction, tools, retrieval,
  and stored chat
- distinguish between:
  - generic compatibility failures
  - shim contract regressions
  - expected unsupported or intentionally out-of-scope cases

To support automation cleanly, shim-specific tests should also carry stable
feature-family metadata from the start, not as a late reporting retrofit.

Suggested families:

- `state`
- `compaction`
- `retrieval`
- `tools`
- `chat_store`
- `routing`
- `lifecycle`
- `limits`

That metadata should live with the test registry so reports, reruns, and future
issue templates can classify failures deterministically.

### 4. Fixture and environment setup notes

Document the runtime prerequisites for the nontrivial flows so failures are not
misclassified as protocol bugs.

Examples:

- seeded vector store and file fixture for retrieval tests
- configured SearxNG for local `web_search`
- configured image backend for local `image_generation`
- configured computer backend for local `computer`
- configured host or Docker backend for local `code_interpreter`
- reachable MCP test server for remote `mcp` scenarios

### 5. Capability manifest and gating

This is the main missing piece if the suite is expected to run cleanly in CI
and in semi-automatic agent loops.

Some shim scenarios depend on optional runtime wiring, not only on endpoint
compatibility. Without explicit gating, those will produce noisy false failures.

Recommended addition:

- define a target capability manifest as a dedicated config file, not as
  ad-hoc profile metadata
- let tests declare required capabilities in addition to broad toggles such as
  `RequiresTools` or `RequiresMemory`
- keep broad suite toggles for coarse pruning, but use capability checks for
  shim-specific runtime dependencies and environment wiring

Suggested file:

- `configs/capabilities_llama_shim.yaml`

The capability manifest should be first-class configuration because it needs
stable validation, reporting, and CI semantics. Hiding it inside profile-level
free-form metadata would make it harder to reason about unsupported features,
disabled backends, and transient dependency outages.

Suggested capability keys:

- `responses.store`
- `responses.retrieve_stream`
- `responses.compaction.auto`
- `conversations.items`
- `chat.store`
- `retrieval.vector_store`
- `tool.file_search.local`
- `tool.web_search.local`
- `tool.image_generation.local`
- `tool.computer.local`
- `tool.code_interpreter.local`
- `tool.mcp.server_url`
- `tool.mcp.approval`
- `tool.tool_search.server`
- `routing.prefer_local`
- `routing.prefer_upstream`
- `routing.local_only`
- `persistence.restart_safe`
- `cleanup.expiry`

The report should distinguish between:

- target does not support this feature
- feature exists but is disabled in this environment
- external dependency for the test is unavailable right now

That distinction matters for CI and for agent-driven triage.

Important implementation rule:

- capability gating should not silently drop deep tests from the run
- broad suite toggles may still exclude whole feature classes early
- once a shim-specific test is selected, unmet capability checks should emit an
  explicit machine-readable non-pass outcome with a normalized reason

Suggested normalized reasons:

- `capability_unsupported`
- `capability_disabled`
- `dependency_unavailable`

That preserves visibility in reports and prevents nightlies or agent reruns from
confusing "not configured here" with "regressed behavior."

### 6. Deterministic fixtures and seeded test assets

The suite should not rely only on live prompt luck.

Add a deterministic asset layer for:

- retrieval corpora
- vector-store ingest files
- image-edit inputs
- computer screenshots
- MCP mock server behavior
- code-interpreter input files

Suggested directories:

- `testdata/llama_shim/retrieval/`
- `testdata/llama_shim/computer/`
- `testdata/llama_shim/image_generation/`
- `testdata/llama_shim/mcp/`
- `testdata/llama_shim/code_interpreter/`

Also add small seed/setup helpers for:

- creating a known vector store with known chunks
- starting or reusing a deterministic MCP fixture server
- preparing image or screenshot inputs before the API flow starts

For V3 fixture-backed parity work, the tester should also be able to compare the
shim's observable replay against committed upstream trace fixtures when those
fixtures exist.

That is especially useful for:

- stored replay flows
- retrieve-stream event families
- tool-specific replay that is intentionally conservative but still expected to
  stay stable

## Proposed Test Matrix

### A. Baseline shim smoke

Purpose:
Prove that the shim still passes the existing generic contract checks before
running deeper implementation-specific scenarios.

Run:

- existing generic `sanity.models`
- existing generic `responses.*` core tests
- existing generic `chat.*` core tests
- optional readiness smoke if the harness later grows a target-ops lane for
  `/readyz`

No new test logic is required here; only a shim-specific suite/profile.

### B. Stateful Responses lifecycle

These tests should validate the parts that the generic suite currently only
touches lightly.

Suggested tests:

- `responses.retrieve.input_items`
  Assert that `GET /v1/responses/{id}/input_items` returns the effective items
  used to generate the response.
- `responses.previous_response.chain`
  Create at least three linked turns and verify that context survives more than
  one follow-up.
- `responses.previous_response.tool_followup`
  Verify that a tool-driven turn can be continued through
  `previous_response_id`.
- `responses.retrieve.stream_replay`
  Create a stored streamed response, then verify that retrieve-stream replays a
  stable generic event sequence and the same terminal content.
- `responses.delete.lifecycle`
  Verify the stored-response delete path if the shim claims it for the selected
  deployment.

Core assertions:

- response IDs remain retrievable across the chain
- stored output survives retrieve and retrieve-stream
- input history is reconstructed from effective state, not only from the last
  request body
- stored state remains coherent after process restart when the deployment claims
  persistent local storage

### C. Conversations lifecycle

The generic `responses.conversations` check is intentionally light. The shim
should get deeper thread-state coverage.

Suggested tests:

- `conversations.create.retrieve`
- `conversations.items.list`
- `conversations.items.append`
- `responses.conversation.followup`

Core assertions:

- conversation creation returns a stable ID
- items can be listed and appended
- a response can continue from `conversation` without using
  `previous_response_id`
- `previous_response_id` and `conversation` remain mutually exclusive where the
  shim documents that rule
- conversation-owned state survives a process restart when local persistence is
  enabled

### D. Automatic compaction

This is the biggest V2-specific gap that generic compatibility smoke will not
cover.

Suggested tests:

- `responses.compact.manual`
  Call `/v1/responses/compact` directly and verify the returned compaction item
  is reusable.
- `responses.compact.auto.previous_response`
  Send a turn with `context_management.compact_threshold`, force compaction, and
  verify the next `previous_response_id` turn continues from compacted state.
- `responses.compact.auto.conversation`
  Same idea, but through `conversation`.
- `responses.compact.stream_replay`
  Verify create-stream and retrieve-stream both replay the stored compaction
  item plus final assistant output consistently.

Core assertions:

- compaction happens before generation when the threshold is crossed
- the compacted output item is stored and replayable
- later follow-up history is trimmed from the latest compaction state rather
  than expanding the full raw history again
- practical replay stays stable across non-stream, stream, and retrieve-stream
- restart does not silently lose the latest compacted lineage when persistence
  is expected

### E. Local retrieval and `file_search`

These tests should validate practical retrieval behavior, not hosted ranking
parity.

Suggested tests:

- `retrieval.files.upload`
- `retrieval.vector_stores.lifecycle`
- `retrieval.search.lexical`
- `retrieval.search.semantic`
- `retrieval.search.hybrid`
- `responses.tool.file_search.local`
- `responses.tool.file_search.include_results`

Core assertions:

- files can be ingested into the test vector store
- search returns relevant chunks from the seeded corpus
- `file_search` can ground a response using local retrieval
- `include=["file_search_call.results"]` returns the shim's documented local
  result subset
- ranking knobs such as `ranker`, `score_threshold`, and `hybrid_search`
  influence behavior functionally where the shim documents them
- seeded retrieval fixtures make the outcome deterministic enough for CI

### F. Local/native tool subsets

These are the areas where `llama_shim` moved well past generic compatibility
smoke.

Suggested tests:

- `responses.tool.web_search.local`
- `responses.tool.image_generation.local`
- `responses.tool.computer.loop`
- `responses.tool.code_interpreter.local`
- `responses.tool.mcp.server_url`
- `responses.tool.mcp.approval`
- `responses.tool.tool_search.server`

Core assertions by tool family:

- `web_search`
  The model emits a local `web_search_call`, the response includes grounded
  sources in the documented subset, and the result is usable in a final answer.
- `image_generation`
  The tool emits an image-generation call, stores the resulting image artifact
  subset, and supports follow-up editing through `previous_response_id` where
  the shim documents it.
- `computer`
  The model emits `computer_call`, accepts follow-up `computer_call_output`
  screenshot items, and can either continue planning or finish with a message.
  Retrieve and `input_items` should preserve the typed computer items.
- `code_interpreter`
  The model emits a local interpreter call, generated files or logs are exposed
  in the documented practical subset, and container/session reuse works across
  `previous_response_id` when the shim claims it.
- `mcp`
  Remote `server_url` tools can list tools, request approval when needed, and
  execute a real MCP call end to end.
- `tool_search`
  Hosted/server-style `tool_search` is usable in the shim's documented subset,
  while client-executed `tool_search` remains outside the local subset if the
  shim documents it as proxy-only.

The tool suite should also include a few deterministic failure-path scenarios:

- disabled backend
- invalid `include`
- unsupported local-only shape
- explicit proxy-only rejection for the paths the shim does not run locally

### G. Tool routing mode matrix

Some of the highest-value shim checks are routing checks rather than pure tool
execution checks.

Suggested matrix:

- `responses.mode.prefer_local`
- `responses.mode.prefer_upstream`
- `responses.mode.local_only`

Apply the matrix to:

- `file_search`
- `web_search`
- `image_generation`
- `computer`
- `code_interpreter`
- remote `mcp`
- `tool_search`

Core assertions:

- `prefer_local` uses the local implementation when configured and supported
- `prefer_upstream` proxies when a real upstream path exists and falls back only
  where the shim documents that behavior
- `local_only` fails explicitly instead of silently proxying

### H. Lifecycle, cancelation, cleanup, and restart safety

These tests are easy to miss, but they matter a lot once the shim is used in
real agent loops.

Suggested tests:

- `responses.cancel.lifecycle`
- `responses.delete.after_retrieve`
- `chat.store.delete.lifecycle`
- `retrieval.vector_store.delete.lifecycle`
- `persistence.responses.restart`
- `persistence.conversations.restart`
- `persistence.chat_store.restart`
- `cleanup.code_interpreter.expiry`
- `cleanup.vector_store.expiry`

Core assertions:

- delete and cancel endpoints behave coherently with follow-up retrieval
- state that is documented as durable survives a restart
- state that is documented as expired or deleted does not reappear
- cleanup jobs do not remove still-live resources

### I. Stored chat compatibility

The generic chat tests validate wire compatibility, but not the shim's local
shadow-store behavior.

Suggested tests:

- `chat.store.create_retrieve`
- `chat.store.stream_retrieve`
- `chat.store.list_delete`
- `chat.store.upstream_bridge`

Core assertions:

- stored chat completions are persisted locally when expected
- retrieve works for both non-stream and stream-created chats
- delete/list semantics are consistent for the local subset
- if the deployment uses an upstream bridge for official stored-chat routes,
  the harness records whether the object came from local storage or the
  upstream-backed path

### J. Failure-shape and contract-edge checks

The shim-specific suite should also validate the important explicit
compatibility boundaries.

Suggested tests:

- `responses.previous_response_and_conversation.conflict`
- `responses.compact.invalid_shape`
- `responses.tool.local_only.unsupported`
- `responses.tool.invalid_include`
- `responses.tool.connector_proxy_only`

Core assertions:

- mutually exclusive state fields fail clearly
- unsupported local tool/runtime combinations fail explicitly
- documented compatibility no-op fields do not pretend to implement full
  semantics
- proxy-only paths are not misreported as local support
- contract-edge tests keep exact error typing and unsupported classification
  stable enough for automation

### K. Limits, concurrency, and isolation

This is not the first delivery phase, but it should be part of the long-term
plan because real agent workloads will stress these paths.

Suggested tests:

- `limits.code_interpreter.concurrent`
- `limits.code_interpreter.generated_bytes`
- `limits.remote_input_file_bytes`
- `limits.vector_store.search_payload`
- `concurrency.responses.parallel_stateful`
- `concurrency.chat.store.parallel`

Core assertions:

- documented hard limits fail with clear API errors
- one overloaded local runtime does not corrupt another request's state
- concurrent requests do not leak stored items or tool outputs across sessions

### L. Docs and example smoke

Because the shim now has practical guides, the tester should eventually gain a
small docs-smoke lane that validates key documented examples against a real
deployment.

Suggested coverage:

- one practical Responses guide example
- one Conversations guide example
- one Retrieval + `file_search` example
- one Computer external-loop example

This does not need to parse every markdown code block automatically at first.
A curated docs-smoke set is enough.

## Recommended Delivery Order

### Phase 1. Shim config layer and capability scaffolding

Deliver:

- `configs/models_llama_shim.yaml`
- `configs/suite_llama_shim.yaml`
- `configs/capabilities_llama_shim.yaml`
- runner support for required capability keys and normalized non-pass reasons
- stable feature-family metadata on shim-specific tests
- README note that this is a target-specific suite layered on top of the generic
  harness

Goal:
Be able to run the existing generic suite cleanly against `llama_shim` with a
single command, while also laying the reporting and gating foundation for deeper
shim scenarios.

### Phase 2. Stateful responses and conversations

Deliver first:

- `responses.retrieve.input_items`
- `responses.previous_response.chain`
- `responses.retrieve.stream_replay`
- conversation lifecycle tests

Reason:
These are foundational and do not require the heaviest external tool runtime.

### Phase 3. Automatic compaction

Deliver next:

- manual compaction
- automatic compaction through `previous_response_id`
- automatic compaction through `conversation`
- stream replay after compaction

Reason:
This is a major V2-specific feature and easy to regress silently.

### Phase 4. Retrieval and tool subsets

Deliver next:

- `file_search`
- `web_search`
- `image_generation`
- `computer`
- `code_interpreter`
- `mcp`
- `tool_search`

Reason:
These flows are valuable, but they require more environment setup and often
need per-tool fixtures. They should land only after capability gating is in
place, otherwise CI noise will dominate the signal.

### Phase 5. Stored chat, lifecycle, and routing matrix

Deliver after the tool family is stable:

- local stored chat lifecycle
- upstream bridge behavior where available
- routing mode matrix across tool families
- cancel/delete/restart safety checks

### Phase 6. Seeded fixtures and environment hardening

Deliver before the suite becomes a serious CI signal:

- deterministic test assets
- fixture setup helpers
- clearer environment documentation for optional backends

Reason:
Capability semantics should already exist by this point. This phase hardens the
tool-heavy flows so the deeper suite becomes stable enough for broader CI use.

### Phase 7. Target-specific reporting polish

Only after the suite exists and has enough volume to justify it.

### Phase 8. Limits, concurrency, and docs smoke

Deliver after the functional core is stable:

- limit-boundary tests
- concurrent stateful request tests
- curated docs-smoke lane

Reason:
These are high-value regressions, but not the right starting point.

## CI/CD and automation model

The current GitHub Actions workflow runs only formatting, `go vet`, and Go unit
tests. That is a good base, but it is not enough for the shim-focused suite.

Recommended lane split:

### 1. Pull request lane

Run on every PR:

- tester unit tests
- config validation
- registry/report serialization tests
- a tiny shim-target smoke subset against a deterministic local fixture target
  if one exists

Goal:
Fast feedback with near-zero flake.

### 2. Merge or post-merge lane

Run on default-branch pushes:

- generic compatibility smoke
- shim stateful smoke
- compaction smoke
- one retrieval smoke

Goal:
Catch mainline regressions quickly without depending on every optional backend.

### 3. Nightly full integration lane

Run on a schedule:

- full `llama_shim` suite with optional backends enabled
- tool subsets
- restart and persistence checks
- routing matrix

Goal:
Exercise the real end-to-end surface, including heavier dependencies.

### 4. Weekly or manual parity lane

Run on schedule or on demand:

- upstream-reference or fixture-backed replay checks
- heavier hosted-parity investigations
- long-running or costlier scenarios

Goal:
Support V3 parity work without making the main CI noisy or expensive.

### 5. Quarantine lane

If a test is known flaky but still valuable:

- keep it out of blocking PR lanes
- run it in nightly or quarantine
- keep its failures visible in reports

Do not hide flaky tests by deleting them from the matrix.

## Autonomous agent flow

The tester is already close to being agent-friendly because it emits
`summary.json`, `raw.csv`, and `full_log.jsonl`, and it already supports
`--rerun-failures-from`.

To make that a real autonomous regression loop, the plan should explicitly rely
on this workflow:

1. run a shim-target suite non-interactively
2. inspect `summary.json` to classify failures by family
3. inspect `full_log.jsonl` for the exact failed steps and evidence
4. rerun only failures with `--rerun-failures-from`
5. optionally run `codex_review` on the reduced failing set
6. emit a compact machine-readable triage summary for issue filing or patch
   generation

Recommended follow-up additions:

- stable per-test feature-family metadata in the registry
- report grouping by family such as `state`, `compaction`, `retrieval`,
  `tools`, `chat_store`, `routing`, `limits`
- a lightweight exit-summary JSON optimized for automation, not only for human
  reading
- optional issue-template output for persistent failures

The feature-family metadata should be introduced with the first shim-specific
tests, not postponed until reporting polish. Otherwise agents will inherit
inconsistent classification across the first few iterations of the suite.

## V3 and V4 strategy

The plan should make future routing obvious:

### V3

Use this tester for API-visible additions and parity tightening such as:

- stronger retrieve-stream or replay fidelity
- exacter hosted tool families when fixture-backed
- expanded stored-chat behavior
- richer compaction parity where the behavior is visible over HTTP

### V4

Use this tester only for extension behavior that is externally visible through
the shim API, for example:

- memory injection visible in response behavior
- richer retrieval-backed knowledge flows
- profile or personalization behavior exposed through normal requests

Do not try to turn this repository into the only place that validates plugin
contracts.

For V4 plugin-style work, split validation into:

- internal contract tests in `llama_shim` for backend interfaces
- external end-to-end smoke in this tester for the user-visible effect

That split is important, otherwise the external harness becomes too slow,
fragile, and under-informative when a plugin fails.

## Suggested Commands

Generic shim smoke:

```bash
go run . \
  --models configs/models_llama_shim.yaml \
  --suite configs/suite_llama_shim.yaml \
  --capabilities configs/capabilities_llama_shim.yaml \
  --profile llama-shim \
  --out-dir reports \
  --json \
  --no-tui
```

Focused rerun for compaction work:

```bash
go run . \
  --models configs/models_llama_shim.yaml \
  --suite configs/suite_llama_shim.yaml \
  --capabilities configs/capabilities_llama_shim.yaml \
  --profile llama-shim \
  --tests responses.compact.manual,responses.compact.auto.previous_response,responses.compact.auto.conversation,responses.compact.stream_replay \
  --out-dir reports \
  --json \
  --no-tui
```

Focused rerun for local tool work:

```bash
go run . \
  --models configs/models_llama_shim.yaml \
  --suite configs/suite_llama_shim.yaml \
  --capabilities configs/capabilities_llama_shim.yaml \
  --profile llama-shim \
  --tests responses.tool.file_search.local,responses.tool.web_search.local,responses.tool.image_generation.local,responses.tool.computer.loop,responses.tool.code_interpreter.local,responses.tool.mcp.server_url,responses.tool.tool_search.server \
  --out-dir reports \
  --json \
  --no-tui
```

## Acceptance Criteria

This plan should be considered implemented when:

- the shim can be exercised through a dedicated suite/profile without touching
  the generic defaults
- the suite covers state, compaction, retrieval, tool subsets, routing, and
  stored chat with explicit test IDs
- capability-gated optional backends do not create noisy false failures in CI
- deterministic seeded assets exist for the stateful and tool-heavy scenarios
- failures clearly identify whether the regression is in generic compatibility
  or shim-specific behavior
- reports are machine-friendly enough for nightly automation and agent-driven
  reruns
- the harness can be used during real `llama_shim` development as a regression
  suite, not only as a generic compatibility smoke test

## Practical Bottom Line

The right first move is not "invent a new tester mode".

The right first move is:

1. keep the generic compatibility harness clean
2. add a dedicated `llama_shim` suite/profile
3. add capability scaffolding and explicit environment-aware reporting before the
   heavy optional-backend scenarios
4. add shim-specific scenario coverage where V2 is deeper than generic OpenAI
   compatibility smoke

If that grows large enough, promote it into a first-class target concept later.
