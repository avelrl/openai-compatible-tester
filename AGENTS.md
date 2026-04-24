## Required verification workflow

Use this order every time:

1. Start with the local docs index at [`docs/llms.txt`](docs/llms.txt).
   Use it as a map of relevant official pages only.
   It is not the source of truth for exact request or response contracts.
2. Use OpenAI Docs MCP tools against `developers.openai.com` and `platform.openai.com`.
   Search first, then fetch the exact page or section you need.
3. For exact API request or response schema, also check the official OpenAPI spec for the relevant endpoint.
   Prefer endpoint-level schema over summary prose when validating exact fields, types, enums, or event shapes.
4. Do a final spot-check on the official site directly for the exact page or endpoint you are validating.
   Prefer only official OpenAI domains.
   Do not substitute a nearby overview or summary page for the exact page or endpoint being implemented.
5. If MCP and the official site disagree, or MCP is thin, ambiguous, or missing exact schema details, treat the current official docs page or OpenAPI spec as the tie-breaker and update the backlog/spec conservatively.
6. For every contract-sensitive decision, record the exact official URL or endpoint spec used so later changes can be audited.
7. If official docs describe a feature as model-dependent, preview, beta, optional, or environment-dependent, do not encode it as an unconditional compatibility requirement.
   Prefer capability-gated coverage or explicit unsupported classification instead.
8. For fixture-backed parity or replay work, fixtures may be captured from the official OpenAI API when docs and spec are insufficient to resolve observable behavior.
   Use only official OpenAI endpoints, keep fixtures minimal, sanitize secrets and account-specific identifiers, and store only the fields needed for stable comparison.
9. Treat captured fixtures as supporting evidence, not as a higher authority than the current official docs page or OpenAPI spec when the contract has changed.

Do not close a compatibility task from memory alone.
