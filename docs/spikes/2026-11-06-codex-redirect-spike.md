# Codex CLI redirect spike — findings

Date: 2026-11-06. Codex CLI 0.166.0, macOS arm64, signed in with ChatGPT (not an API key).
Method: throwaway Go capture server on 127.0.0.1:8799 that logged requests and replied with a minimal Responses-API SSE stream. Codex was run with command-line `-c` overrides only; `~/.codex/config.toml` was not changed. Prompts were synthetic ("say hi", "run the command"). Nothing was sent to the real upstream.

## Result: redirect works with ChatGPT login

```
codex exec \
  -c 'model_provider="spike"' \
  -c 'model_providers.spike={name="spike",base_url="http://127.0.0.1:8799/v1",wire_api="responses",requires_openai_auth=true}' \
  '<prompt>'
```

The same two keys can live in `~/.codex/config.toml` (or a profile) for permanent use. Note: run with stdin closed (`</dev/null`) when scripting, otherwise `codex exec` waits for stdin.

## What the proxy will see

- `GET /v1/models?client_version=...` (twice at startup): must be passed through.
- `POST /v1/responses`, `Accept: text/event-stream`, plain HTTP + SSE (no websocket with a custom provider).
- Auth headers are sent to the custom base URL when `requires_openai_auth=true`: `Authorization: Bearer <ChatGPT token>`, `Chatgpt-Account-Id`. The proxy must forward these untouched, and must never log them.
- Other headers: `Session-Id`, `Thread-Id`, `X-Codex-Turn-Metadata` (JSON with ids, model, sandbox mode), `X-Codex-Beta-Features`.
- Request body keys: `model, stream, input[], tools[], tool_choice, parallel_tool_calls, reasoning, store(false), include(["reasoning.encrypted_content"]), prompt_cache_key, text, client_metadata`. `instructions` was empty; the system prompt is in `input[]` as `developer` messages (~33 KB of the ~58 KB body).
- `input[]` items seen: `message` (roles developer/user; content parts `input_text`), `function_call` (`name`, `arguments` as a JSON *string*, `call_id`), `function_call_output` (`call_id`, `output` as a plain string).
- Tools: `function` (exec_command, write_stdin, MCP resource tools, ...), `custom` (apply_patch), `tool_search`, `web_search`.

## Tool-call round trip (the restoration case)

Fake upstream streamed `response.output_item.added` (function_call) -> two `response.function_call_arguments.delta` events that split the value `10.0.0.7` across chunks -> `response.output_item.done` carrying the full `arguments` string -> `response.completed`.

Codex executed `echo host=10.0.0.7 done` locally and, in the next `POST /v1/responses`, resent the whole history: the earlier user message, the `function_call` (with arguments), and the `function_call_output` (shell output, which also contained local paths and shell-startup noise).

Implications:
1. Restoration must happen in `function_call_arguments.delta` events AND the `output_item.done` event (values can be split across delta chunks), so the streaming restorer must buffer across deltas.
2. The next request resends everything, including the restored real value in `function_call.arguments` and in `function_call_output.output`; it must be re-sanitized every turn. Tool output is a major leak path (paths, hostnames, IPs in logs).
3. The system prompt and environment context also travel in every request and can contain local paths and the username; they need scanning too.

## Not yet verified

- The real upstream for ChatGPT auth appears to be `https://chatgpt.com/backend-api/codex` (string found in the Codex binary; not exercised). The proxy must map `/v1/responses` to the upstream path Codex would normally use; confirm with a harmless synthetic prompt when the proxy exists.
- Reasoning items (`reasoning.encrypted_content`) are opaque and must pass through untouched; their stream events were not observed.
- Image inputs, compaction requests (`remote_compaction_v2`), and websocket mode were not observed.
