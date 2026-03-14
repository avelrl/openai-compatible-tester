# Reference Results

Public reference results for known-good OpenAI-compatible profiles.

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

## Notes

- `openrouter-kimi-k2.5` keeps the tuned overrides directly in [models_openrouter_paid.yaml](../configs/models_openrouter_paid.yaml).
- Public references should come from OpenRouter-style public endpoints, not private gateway runs.
- Private runs are better used for regression/debugging, not as canonical examples.
