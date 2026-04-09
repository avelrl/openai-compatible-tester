# Official OpenAI Notes

This repo uses the official OpenAI API as the strict baseline, but GPT-5-family
request compatibility is model-specific and surface-specific. A red run against
`https://api.openai.com` can still be a harness/config bug if we send the wrong
payload for that exact model.

## Validated baselines

- Date: `2026-04-09`
- Endpoint: `https://api.openai.com`
- Profile: `openai-gpt-5.4-nano`
- Reference mode: `strict`
- Reference run ID: `openai_gpt_5_4_nano_strict_reference_green_20260409_225419`
- Result: `chat=READY`, `responses=READY`
- Profile: `openai-gpt-5.4-mini`
- Reference mode: `strict`
- Reference run ID: `openai_gpt_5_4_mini_strict_reference_green_20260409_225610`
- Result: `chat=READY`, `responses=READY`

## Rules confirmed from OpenAI docs

- [GPT-5.4 parameter compatibility](https://developers.openai.com/api/docs/guides/latest-model#gpt-54-parameter-compatibility):
  `temperature`, `top_p`, and `logprobs` are not universally valid across GPT-5.x
  requests. Support depends on the exact model and reasoning mode.
- [Reasoning guide](https://developers.openai.com/api/docs/guides/reasoning#get-started-with-reasoning):
  `reasoning.effort` values are model-dependent.
- [Choosing models and APIs](https://developers.openai.com/api/docs/guides/text#choosing-models-and-apis):
  OpenAI recommends `Responses API` as the default for reasoning models.
- [Structured outputs](https://developers.openai.com/api/docs/guides/structured-outputs#structured-outputs-vs-json-mode):
  structured outputs / JSON mode are supported on both `Responses` and `Chat Completions`.

## Practical harness rules

- Keep generic suite defaults conservative. Do not enable chat reasoning by default.
- Put GPT-5.x tuning in `models_*.yaml` profile overrides, not in shared `suite*.yaml` defaults.
- Treat canonical OpenAI `unsupported parameter` / `unsupported value` responses as unsupported feature signals, not as proof that the provider broke the spec.

## Known-good tuning for `gpt-5.4-nano`

- Use `chat_max_tokens_param: max_completion_tokens`.
- Omit chat `reasoning`.
- Do not send `temperature` unless the exact model/doc combination allows it.
- Use only `responses.reasoning.effort` values supported by the model. For this validated profile, `low` is the safe preset.

## Known-good tuning for `gpt-5.4-mini`

- Use `chat_max_tokens_param: max_completion_tokens`.
- Keep shared chat reasoning disabled; this validated profile passes chat without per-test chat reasoning overrides.
- `responses.reasoning.effort: low` is accepted across the validated Responses tests.

## Rerun policy

Older runs should be treated as invalidated and rerun when failures were dominated by:

- `unsupported parameter`
- `unsupported value`
- `unknown_parameter`
- chat `reasoning`
- `temperature` / `top_p` / `logprobs`
- `max_tokens` vs `max_completion_tokens`
