# openai-compatible-tester

A practical compatibility harness for OpenAI-style APIs, focused on the behaviors that actually break coding agents and assistant frameworks.

It validates both **Chat Completions** and **Responses** surfaces and checks more than simple transport reachability:
- exact basic responses
- SSE streaming
- structured outputs
- tool calling
- follow-up turns after tool execution
- compatibility with known clients such as Codex CLI and OpenAI-compatible agent frameworks

## What This Is

`openai-compatible-tester` is meant to answer a practical question:

`Can this endpoint/model/gateway be trusted for the API features my client actually needs?`

It is designed for real integration work against OpenAI-compatible providers, proxies, and local runtimes such as gateways, routers, and model servers.

The output is intentionally operational:
- `PASS` when behavior works
- `FAIL` when behavior is broken
- `UNSUPPORTED` when a feature is not implemented or explicitly rejected
- `READY` / `READY WITH LIMITATIONS` / `NOT READY` for agent-facing API surfaces

This is a third-party tool and is not affiliated with OpenAI.

## What It Tests

- `GET /models`
- `POST /chat/completions`
- `POST /responses`
- streaming over SSE
- strict structured output checks
- tool calling and the second turn after tool execution
- memory/follow-up flows
- optional retrieval/conversation-style endpoints
- compatibility against curated client requirements from `configs/clients.yaml`

## Non-Goals

- It is **not** a bug-for-bug verifier of full OpenAI parity.
- It is **not** a benchmark of model intelligence or coding quality.
- It does **not** replace one real smoke test in the target client.
- Some compatibility rules in `configs/clients.yaml` are curated rather than formally guaranteed by upstream projects.

## Requirements

- Go `1.25+`
- An endpoint that exposes an OpenAI-style API
- An API key only if the target requires authentication

## Setup

1) Create a local `.env` from `.env.example` (optional) or export the variables in your shell:

```bash
export OPENAI_API_KEY=your_key
export BASE_URL=https://your-openai-compatible.example.com
```

Prefer `BASE_URL` without `/v1`; the loader also strips a trailing `/v1` automatically.
If your target does not require authentication, leave `OPENAI_API_KEY` unset.

Model IDs come from the selected `models.yaml` profile, so `MODEL_CHAT` and `MODEL_RESP` are not used by the loader.

2) Review the YAML configs in `configs/`:
- `configs/endpoints.yaml`
- `configs/models.yaml`
- `configs/suite.yaml`
- `configs/clients.yaml`

The default `endpoints.yaml` and `models.yaml` are neutral templates. Provider-specific presets can live alongside them as separate files.

Config precedence: **flags > environment variables > .env > YAML defaults**.

## Quick Start

Install:

```bash
go install github.com/avelrl/openai-compatible-tester@latest
```

Generic run:

```bash
go run . --no-tui --out-dir reports --json
```

Provider-specific run:

```bash
go run . \
  --models configs/models_openrouter_paid.yaml \
  --suite configs/suite_openrouter_paid.yaml \
  --profile openrouter-gpt5-mini \
  --out-dir reports \
  --json
```

## Run

Interactive TUI (default):

```bash
go run .
```

After the test run completes, the TUI stays open for inspection. Press `q` to exit.

Non-interactive (CI-friendly):

```bash
go run . --no-tui --out-dir reports --json
```

Single profile:

```bash
go run . --profile default
```

Custom known-client matrix:

```bash
go run . --clients configs/clients.yaml
```

## Reports

Each run writes to its own timestamped directory.

Default layout with `--out-dir reports`:
- `reports/<profile_or_multi>_<timestamp>/raw.csv` - one row per test execution
- `reports/<profile_or_multi>_<timestamp>/summary.md` - human-readable summary
- `reports/<profile_or_multi>_<timestamp>/full_log.jsonl` - full per-test request/response log (JSONL)
- `reports/<profile_or_multi>_<timestamp>/summary.json` - optional (`--json`)
- `reports/<profile_or_multi>_<timestamp>/codex_review.md` - optional (`codex_review.enabled=true`)

`summary.md` also includes:
- `Agent readiness` split by `chat` and `responses`
- `Known client compatibility` based on `configs/clients.yaml`
- `Basic text exactness` separated from protocol compatibility

## Status meanings

- **PASS**: expected behavior observed and validations succeeded
- **UNSUPPORTED**: endpoint missing (404/405) or optional feature rejected as unknown parameter
- **FAIL**: schema mismatch, invalid JSON, tool call not executed, stream didn't terminate, memory mismatch, etc.
- **TIMEOUT**: client-side deadline exceeded / request timed out

Exit codes:
- `0` - all required tests pass (UNSUPPORTED where allowed)
- `1` - any FAIL/TIMEOUT (or flaky tests if `fail_on_flaky=true`)
- `2` - configuration/usage error

`codex_review` is advisory: it enriches the report, but does not change the compatibility exit code.

## Per-test overrides (suite.yaml)

`configs/suite.yaml` supports per-test knobs under `tests:` (keyed by test id).

Common use-case: make tool calling less flaky behind LiteLLM by giving it a longer timeout and
explicit tool-choice + headers:

```yaml
report:
  snippet_limit_bytes: 4096

tests:
  responses.tool_call:
    timeout_seconds: 180
    litellm_headers:
      x-litellm-timeout: 180
    tool_choice_mode: forced   # forced|required|auto
    forced_tool_name: add
    parallel_tool_calls: false
    reasoning_effort: minimal  # minimal|low|medium|high|omit
    strict_mode: false

  chat.tool_call:
    timeout_seconds: 180
    litellm_headers:
      x-litellm-timeout: 180
    tool_choice_mode: forced
    forced_tool_name: add
    parallel_tool_calls: false
    max_tokens: 64
    strict_mode: false
```

Notes:
- `litellm_headers` are injected into HTTP requests (suite-level `litellm_headers` + per-test overrides).
- `responses.tool_call.stream=true` enables SSE mode for step-1 and sets `x-litellm-stream-timeout`
  (default 30s; configurable via `stream_timeout_seconds` or directly via headers).
- `responses.tool_call.max_output_tokens` is optional. Some LiteLLM/vLLM backends reject this field;
  leave it unset to omit it from requests.
- `report.snippet_limit_bytes` limits stored request/response snippets in reports and TUI snapshots.
- `chat_reasoning.enabled=false` by default. If enabled, chat requests will include `reasoning.effort`
  from `models.yaml` (non-standard; enable only if your proxy supports it).

## Notes

- Streaming tests use SSE parsing and require a proper `done` marker.
- Tool-calling tests use two-step flows and validate exact numeric answers.
- Warmup passes are excluded from summary stats.
- `configs/clients.yaml` is a curated, editable map of well-known agents/assistants and the minimum tests they need.
  Update it as those clients change API expectations over time.
- Canonical manual `curl` examples live in `details.md`. `api.http` is only a safe local scratch template.
- `full_log.jsonl` stores step-based traces for multi-step tests, so tool/memory failures are easier to debug.

## Known clients config

`configs/clients.yaml` is evaluated per profile and per client target. Each target may declare one or more `modes`
such as `chat`, `responses`, or a client-specific integration mode. The report picks the best mode automatically.
Use `verified_on`, `source`, and `confidence` to distinguish contract-level doc coverage from curated assumptions.

Minimal shape:

```yaml
targets:
  - id: codex-cli
    name: Codex CLI
    category: coding_agent
    docs_url: "https://developers.openai.com/codex/config-reference/"
    modes:
      - name: responses
        api: responses
        verified_on: "2026-03-13"
        source: "official_docs"
        confidence: "high"
        required_tests:
          - responses.basic
          - responses.stream
          - responses.tool_call.required
        optional_tests:
          - responses.structured.json_schema
        unsupported_ok:
          - responses.memory.prev_id
```
