# Reference Results

Public reference results for known-good OpenAI-compatible profiles, plus summarized
official OpenAI baselines for private-key endpoints.
For `chat` references, the verdict is scoped to the tested core agent paths unless noted otherwise.

These are intentionally summarized here instead of committing raw `reports/` output:
- `reports/` is gitignored
- raw logs are noisy and provider-route specific
- private/local runs may expose internal base URLs that are not useful in a public repo

## Green references

| Profile | Surface | Verdict | Notes |
| --- | --- | --- | --- |
| `openrouter-gpt5-mini` | `chat` | `READY` | Full green baseline for strict OpenAI-compatible behavior. |
| `openrouter-gpt5-mini` | `responses` | `READY` | Full green baseline for `Responses API`, including stream, tools, and structured output. |
| `openrouter-deepseek-3.2` | `chat` | `READY` | Full green reference without profile-specific tuning. |
| `openrouter-deepseek-3.2` | `responses` | `READY` | Full green reference without profile-specific tuning. |
| `openrouter-kimi-k2.5` | `chat` | `READY` | Green with profile-specific overrides for tool and structured tests. |
| `openrouter-kimi-k2.5` | `responses` | `READY` | Green with the same profile. |
| `nvidia-kimi-k2-instruct` | `chat` | `READY` | Green chat-only reference on the public NVIDIA hosted endpoint. |
| `nvidia-nemotron-3-super-120b-a12b` | `chat` | `READY` | Core agent chat paths are green on the public NVIDIA hosted endpoint; `chat.error_shape` still returns `500` on invalid payloads. |
| `nvidia-qwen3-next-80b-a3b-instruct` | `chat` | `READY` | Green chat reference on the public NVIDIA hosted endpoint with profile-specific `system` role overrides for `chat.basic` and `chat.stream`. |
| `nvidia-kimi-k2.5` | `chat` | `READY?` | Provisional chat reference on the public NVIDIA hosted endpoint; current tuned profile is green, but `chat.structured.json_schema` has flaked in neighboring runs. |

## Official OpenAI baselines

These are not raw committed reports because the underlying runs use private API keys.
The canonical tuning notes live in [openai-official-notes.md](./openai-official-notes.md).

| Profile | Surface | Verdict | Notes |
| --- | --- | --- | --- |
| `openai-gpt-5.4-nano` | `chat` | `READY` | Validated on `2026-04-09` against `https://api.openai.com`; strict baseline run id `openai_gpt_5_4_nano_strict_reference_green_20260409_225419`. |
| `openai-gpt-5.4-nano` | `responses` | `READY` | Same strict run; no unsupported features or incompatibilities detected across the full suite. |
| `openai-gpt-5.4-mini` | `chat` | `READY` | Validated on `2026-04-09` against `https://api.openai.com`; strict baseline run id `openai_gpt_5_4_mini_strict_reference_green_20260409_225610`. |
| `openai-gpt-5.4-mini` | `responses` | `READY` | Same strict run; no unsupported features or incompatibilities detected across the full suite. |

## Tested, not references

| Profile | Status | Why not reference |
| --- | --- | --- |
| `openrouter-qwen3.5-397b-a17b` | Nearly working | Route-sensitive behavior, especially on structured output. |
| `openrouter-glm-5` | Unstable | Tool and structured paths vary between runs/providers. |

## Reproducing public references

Use:

```bash
go run . \
  --models configs/models_openrouter_paid.yaml \
  --suite configs/suite_openrouter_paid.yaml \
  --profile openrouter-gpt5-mini \
  --out-dir reports
```

```bash
go run . \
  --models configs/models_openrouter_paid.yaml \
  --suite configs/suite_openrouter_paid.yaml \
  --profile openrouter-deepseek-3.2 \
  --out-dir reports
```

```bash
go run . \
  --models configs/models_openrouter_paid.yaml \
  --suite configs/suite_openrouter_paid.yaml \
  --profile openrouter-kimi-k2.5 \
  --out-dir reports
```

```bash
go run . \
  --base-url https://integrate.api.nvidia.com \
  --models configs/models_nvidia.yaml \
  --profile nvidia-kimi-k2-instruct \
  --tests sanity.models,chat.basic,chat.stream,chat.tool_call,chat.tool_call.required,chat.error_shape,chat.structured.json_schema,chat.structured.json_object \
  --out-dir reports
```

```bash
go run . \
  --base-url https://integrate.api.nvidia.com \
  --models configs/models_nvidia.yaml \
  --profile nvidia-nemotron-3-super-120b-a12b \
  --tests sanity.models,chat.basic,chat.stream,chat.tool_call,chat.tool_call.required,chat.structured.json_schema,chat.structured.json_object \
  --out-dir reports
```

```bash
go run . \
  --base-url https://integrate.api.nvidia.com \
  --models configs/models_nvidia.yaml \
  --profile nvidia-qwen3-next-80b-a3b-instruct \
  --tests sanity.models,chat.basic,chat.stream,chat.tool_call,chat.tool_call.required,chat.error_shape,chat.structured.json_schema,chat.structured.json_object \
  --out-dir reports
```

```bash
go run . \
  --base-url https://integrate.api.nvidia.com \
  --models configs/models_nvidia.yaml \
  --profile nvidia-kimi-k2.5 \
  --tests sanity.models,chat.basic,chat.stream,chat.tool_call,chat.tool_call.required,chat.error_shape,chat.structured.json_schema,chat.structured.json_object \
  --out-dir reports
```

## Notes

- `openrouter-kimi-k2.5` keeps the tuned overrides directly in [models_openrouter_paid.yaml](../configs/models_openrouter_paid.yaml).
- `nvidia-kimi-k2-instruct` is currently a `chat`-only reference; `responses` is not a public green reference for this endpoint.
- `nvidia-nemotron-3-super-120b-a12b` is a practical `chat` reference for core agent paths, but not a strict error-semantics reference because invalid payloads currently return `500` instead of `4xx`.
- `nvidia-qwen3-next-80b-a3b-instruct` keeps `chat.basic` and `chat.stream` on `instruction_role: system` in [models_nvidia.yaml](../configs/models_nvidia.yaml).
- `nvidia-kimi-k2.5` currently uses tuned overrides in [models_nvidia.yaml](../configs/models_nvidia.yaml) for tool-calling and structured output, plus a longer 429 fallback in the suite config. Keep it marked with a question mark for now because `chat.structured.json_schema` has failed in some nearby runs.
- Public references should come from OpenRouter-style public endpoints, not private gateway runs.
- Private runs are better used for regression/debugging, not as canonical examples.
- Official OpenAI baselines are still useful as strict payload-shape references, especially for GPT-5-family parameter compatibility.
