## 0) Environment variables (single template)

```bash
export BASE_URL="https://YOUR-OPENAI-COMPAT.example.com"   # without /v1
export OPENAI_API_KEY="YOUR_KEY"
export MODEL_CHAT="gpt-5"     # or your model name for /chat/completions
export MODEL_RESP="gpt-5"     # or your model name for /responses
```

*(If your BASE_URL already includes `/v1`, just remove `/v1` from the requests below.)*

---

## A) Models / sanity

### A1. Model list (quick “is the server alive?” check)

```bash
curl -sS "$BASE_URL/v1/models" \
  -H "Authorization: Bearer $OPENAI_API_KEY"
```

---

## B) Responses API (main set)

### B1. Minimal Responses (text)

```bash
curl -sS "$BASE_URL/v1/responses" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"model\": \"$MODEL_RESP\",
    \"input\": \"ping: answer with OK\"
  }"
```

### B2. Store + retrieve (check `store` and GET by `response_id`)

`Responses` are stored by default (`store: true`). ([OpenAI Platform][1])

```bash
RESP_ID=$(curl -sS "$BASE_URL/v1/responses" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"model\": \"$MODEL_RESP\",
    \"store\": true,
    \"input\": \"Say OK and nothing else\"
  }" | jq -r '.id')

curl -sS "$BASE_URL/v1/responses/$RESP_ID" \
  -H "Authorization: Bearer $OPENAI_API_KEY"
```

*(If you don't want `jq`, the CLI can parse this in code later.)*

### B3. Streaming (SSE)

Enabled with `stream: true`. ([OpenAI Platform][2])

```bash
curl -N -sS "$BASE_URL/v1/responses" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"model\": \"$MODEL_RESP\",
    \"stream\": true,
    \"input\": \"Stream test: print 'HELLO' one char at a time\"
  }"
```

### B4. Structured Outputs (json_schema) — Responses

Format via `text.format` (`type: json_schema`). ([OpenAI Platform][3])

```bash
curl -sS "$BASE_URL/v1/responses" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "'"$MODEL_RESP"'",
    "input": [
      {"role":"system","content":"Return JSON strictly according to the schema."},
      {"role":"user","content":"Generate an object with status ok and the number 42."}
    ],
    "text": {
      "format": {
        "type": "json_schema",
        "name": "simple_status",
        "schema": {
          "type": "object",
          "properties": {
            "status": {"type":"string"},
            "value": {"type":"integer"}
          },
          "required": ["status","value"],
          "additionalProperties": false
        },
        "strict": true
      }
    }
  }'
```

### B5. JSON mode (json_object) — Responses

JSON mode requires the word “JSON” to appear explicitly somewhere in the context. ([OpenAI Platform][3])

```bash
curl -sS "$BASE_URL/v1/responses" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "'"$MODEL_RESP"'",
    "input": [
      {"role":"system","content":"You output JSON only."},
      {"role":"user","content":"Return JSON like {\"ok\":true}."}
    ],
    "text": { "format": { "type": "json_object" } }
  }'
```

### B6. Tool calling (function tools) — Responses, 2 steps

Flow: request → get `function_call` → send `function_call_output` → get final text. ([OpenAI Platform][4])

**Step 1: ask the model to call `add`**

```bash
RESP1=$(curl -sS "$BASE_URL/v1/responses" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "'"$MODEL_RESP"'",
    "tool_choice": "required",
    "tools": [
      {
        "type": "function",
        "name": "add",
        "description": "Add two integers",
        "parameters": {
          "type":"object",
          "properties": {
            "a":{"type":"integer"},
            "b":{"type":"integer"}
          },
          "required":["a","b"],
          "additionalProperties": false
        },
        "strict": true
      }
    ],
    "input": [
      {"role":"user","content":"Compute 40+2 using the add tool. Then reply with just the number."}
    ]
  }')

echo "$RESP1" | jq
CALL_ID=$(echo "$RESP1" | jq -r '.output[] | select(.type=="function_call") | .call_id')
ARGS=$(echo "$RESP1" | jq -r '.output[] | select(.type=="function_call") | .arguments')
echo "call_id=$CALL_ID args=$ARGS"
```

**Step 2: send the tool result back**

```bash
curl -sS "$BASE_URL/v1/responses" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "'"$MODEL_RESP"'",
    "tools": [
      {
        "type": "function",
        "name": "add",
        "description": "Add two integers",
        "parameters": {
          "type":"object",
          "properties": {
            "a":{"type":"integer"},
            "b":{"type":"integer"}
          },
          "required":["a","b"],
          "additionalProperties": false
        },
        "strict": true
      }
    ],
    "instructions": "Reply with just the number.",
    "input": '"$(echo "$RESP1" | jq '.output + [{"type":"function_call_output","call_id":"'"$CALL_ID"'","output":"{\"result\":42}"}]')"'
  }'
```

### B7. “Memory” via `previous_response_id` (multi-turn chaining)

`previous_response_id` cannot be used together with `conversation`. ([OpenAI Platform][1])
Also note: `instructions` are not automatically carried over between responses. ([OpenAI Platform][1])

```bash
RESP_ID=$(curl -sS "$BASE_URL/v1/responses" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"model\": \"$MODEL_RESP\",
    \"input\": \"Remember: my code = 123. Reply OK\"
  }" | jq -r '.id')

curl -sS "$BASE_URL/v1/responses" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"model\": \"$MODEL_RESP\",
    \"previous_response_id\": \"$RESP_ID\",
    \"input\": \"What was my code? Reply with just the number.\"
  }"
```

### B8. “Memory” via Conversations API (persistent context)

Conversations let you store/reuse state between `responses` calls. ([OpenAI Platform][5])

**Create a conversation**

```bash
CONV_ID=$(curl -sS "$BASE_URL/v1/conversations" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "items": [
      {"type":"message","role":"system","content":"You are a test assistant."},
      {"type":"message","role":"user","content":"Remember: code=777. Reply OK."}
    ]
  }' | jq -r '.id')

echo "conv_id=$CONV_ID"
```

**Use the conversation in responses**

```bash
curl -sS "$BASE_URL/v1/responses" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"model\": \"$MODEL_RESP\",
    \"conversation\": \"$CONV_ID\",
    \"input\": [{\"role\":\"user\",\"content\":\"What is the code? Reply with just the number.\"}]
  }"
```

---

## C) Chat Completions (coverage for the “legacy”/compatible API)

### C1. Minimal chat/completions

```bash
curl -sS "$BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"model\": \"$MODEL_CHAT\",
    \"messages\": [
      {\"role\":\"developer\",\"content\":\"Reply with exactly OK\"},
      {\"role\":\"user\",\"content\":\"ping\"}
    ]
  }"
```

### C2. Streaming chat/completions

```bash
curl -N -sS "$BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"model\": \"$MODEL_CHAT\",
    \"stream\": true,
    \"messages\": [
      {\"role\":\"developer\",\"content\":\"Stream one word: HELLO\"},
      {\"role\":\"user\",\"content\":\"go\"}
    ]
  }"
```

(Streaming in chat is SSE as well. ([OpenAI Platform][6]))

### C3. Tool calling — Chat, 2 steps

In Chat, the tools schema is: `tools: [{ "type":"function", "function": {...}}]`. ([OpenAI Platform][6])

**Step 1**

```bash
CHAT1=$(curl -sS "$BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "'"$MODEL_CHAT"'",
    "tool_choice": "required",
    "tools": [
      {
        "type": "function",
        "function": {
          "name": "add",
          "description": "Add two integers",
          "parameters": {
            "type":"object",
            "properties":{"a":{"type":"integer"},"b":{"type":"integer"}},
            "required":["a","b"],
            "additionalProperties": false
          },
          "strict": true
        }
      }
    ],
    "messages": [
      {"role":"user","content":"Add 40 and 2 using add, then reply with just the number."}
    ]
  }')

TOOL_CALL_ID=$(echo "$CHAT1" | jq -r '.choices[0].message.tool_calls[0].id')
ARGS=$(echo "$CHAT1" | jq -r '.choices[0].message.tool_calls[0].function.arguments')
echo "tool_call_id=$TOOL_CALL_ID args=$ARGS"
```

**Step 2**

```bash
curl -sS "$BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "'"$MODEL_CHAT"'",
    "messages": [
      {"role":"user","content":"Add 40 and 2 using add, then reply with just the number."},
      '"$(echo "$CHAT1" | jq '.choices[0].message')"',
      {"role":"tool","tool_call_id":"'"$TOOL_CALL_ID"'","content":"{\"result\":42}"}
    ]
  }'
```

### C4. “Memory” in Chat (just message history)

```bash
curl -sS "$BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"model\": \"$MODEL_CHAT\",
    \"messages\": [
      {\"role\":\"user\",\"content\":\"Remember: code=999. Reply OK\"},
      {\"role\":\"assistant\",\"content\":\"OK\"},
      {\"role\":\"user\",\"content\":\"What is the code? Reply with just the number.\"}
    ]
  }"
```

---

## D) Errors/compatibility (so the CLI can distinguish FAIL vs UNSUPPORTED)

### D1. Invalid model → expect 4xx and a normal error body

```bash
curl -sS "$BASE_URL/v1/responses" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "definitely-not-a-real-model",
    "input": "ping"
  }'
```

### D2. Check “endpoint missing” (if the server is Chat-only)

```bash
curl -i -sS "$BASE_URL/v1/responses" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"model\":\"$MODEL_RESP\",\"input\":\"ping\"}"
```

If 404/405, the CLI marks it as **SKIP/UNSUPPORTED**, not “the model is bad”.

---
