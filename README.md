# Kimi Multi-Protocol Emulator

English | [中文](README_ZH.md)

This is a Kimi-focused protocol emulator. It exposes a Kimi-native passthrough namespace plus OpenAI Chat Completions, OpenAI Responses, Anthropic Messages, and Gemini Generate Content compatible APIs, then emulates their request and response semantics on top of Kimi Chat Completions as faithfully as possible.

It is configured with command-line flags for local runs and environment variables for container deployment.

## Configuration and Usage

For local binary runs:

```bash
go build -trimpath -ldflags="-s -w" -o kimi-compatible ./cmd/server
```

```bash
./kimi-compatible \
  --listen :8080 \
  --api-token sk-local-test \
  --kimi-api-key sk-your-kimi-key \
  --kimi-base-url https://api.moonshot.cn/v1 \
  --kimi-model kimi-k2.7-code \
  --kimi-models kimi-k2.7-code,kimi-k2.6,kimi-k2.5,moonshot-v1-128k \
  --kimi-http-timeout 120 \
  --verify-ssl=true \
  --debug-log-body=false
```

For container deployment, use `docker.env.example`:

```bash
cp docker.env.example docker.env
docker build -t kimi-compatible:latest .
docker run -itd \
  --name kimi-compatible \
  -p 8080:8080 \
  --env-file docker.env \
  --restart always \
  kimi-compatible:latest
```

Container environment reference:

| Environment variable | Equivalent flag |
| --- | --- |
| `LISTEN` | `--listen` |
| `API_TOKEN` | `--api-token` |
| `KIMI_API_KEY` | `--kimi-api-key` |
| `KIMI_BASE_URL` | `--kimi-base-url` |
| `KIMI_MODEL` | `--kimi-model` |
| `KIMI_MODELS` | `--kimi-models` |
| `KIMI_HTTP_TIMEOUT` | `--kimi-http-timeout` |
| `KIMI_MAX_IDLE_CONNS` | `--kimi-max-idle-conns` |
| `KIMI_MAX_IDLE_CONNS_PER_HOST` | `--kimi-max-idle-conns-per-host` |
| `KIMI_MAX_CONNS_PER_HOST` | `--kimi-max-conns-per-host` |
| `STORE_MAX_RESPONSES` | `--store-max-responses` |
| `STORE_MAX_CHAT_COMPLETIONS` | `--store-max-chat-completions` |
| `STORE_MAX_CONVERSATIONS` | `--store-max-conversations` |
| `MAX_REQUEST_BODY_BYTES` | `--max-request-body-bytes` |
| `READ_HEADER_TIMEOUT` | `--read-header-timeout` |
| `IDLE_TIMEOUT` | `--idle-timeout` |
| `DEBUG_PPROF` | `--debug-pprof` |
| `VERIFY_SSL` | `--verify-ssl` |
| `DEBUG_LOG_BODY` | `--debug-log-body` |

## Compatible Endpoints

### Kimi Native Chat Completions

| Endpoint | Description |
| --- | --- |
| `POST /kimi/v1/chat/completions` | Kimi-native Chat Completions passthrough. |

### OpenAI Chat Completions

| Endpoint | Description |
| --- | --- |
| `POST /v1/chat/completions` | Create OpenAI-compatible Chat Completions and forward them to Kimi Chat Completions through the compatibility adapter. |
| `GET /v1/chat/completions` | List locally stored Chat Completions. |
| `GET /v1/chat/completions/{completion_id}` | Retrieve one locally stored Chat Completion. |
| `POST /v1/chat/completions/{completion_id}` | Update metadata for a locally stored Chat Completion. |
| `DELETE /v1/chat/completions/{completion_id}` | Delete a locally stored Chat Completion. |
| `GET /v1/chat/completions/{completion_id}/messages` | List messages for a locally stored Chat Completion. |

### OpenAI Responses

| Endpoint | Description |
| --- | --- |
| `POST /v1/responses` | Create Responses and forward them to Kimi Chat Completions. |
| `GET /v1/responses/{response_id}` | Retrieve one locally stored Response. |
| `DELETE /v1/responses/{response_id}` | Delete a locally stored Response. |
| `GET /v1/responses/{response_id}/input_items` | List input items for a locally stored Response. |
| `POST /v1/responses/{response_id}/cancel` | Cancel a Response according to local state semantics. |
| `POST /v1/responses/input_tokens` | Count input tokens through Kimi `/v1/tokenizers/estimate-token-count`. |
| `POST /v1/responses/compact` | Use Kimi for best-effort context compaction and summarization. |

### OpenAI Conversations

| Endpoint | Description |
| --- | --- |
| `POST /v1/conversations` | Create a local Conversation. |
| `GET /v1/conversations/{conversation_id}` | Retrieve a local Conversation. |
| `POST /v1/conversations/{conversation_id}` | Append to or update a local Conversation. |
| `DELETE /v1/conversations/{conversation_id}` | Delete a local Conversation. |

### Anthropic Messages

| Endpoint | Description |
| --- | --- |
| `POST /v1/messages` | Create an Anthropic Messages response and forward it to Kimi Chat Completions. |
| `POST /v1/messages/count_tokens` | Count Anthropic Messages tokens through Kimi token estimation. |

### Gemini Generate Content

| Endpoint | Description |
| --- | --- |
| `POST /v1beta/models/{model}:generateContent` | Create a Gemini Generate Content response and forward it to Kimi Chat Completions. |
| `POST /v1beta/models/{model}:streamGenerateContent` | Create a streaming Gemini Generate Content response. |
| `POST /v1beta/models/{model}:countTokens` | Count Gemini v1beta tokens through Kimi token estimation. |
| `POST /v1/models/{model}:generateContent` | Create a Gemini v1 Generate Content response and forward it to Kimi Chat Completions. |
| `POST /v1/models/{model}:streamGenerateContent` | Create a streaming Gemini v1 Generate Content response. |
| `POST /v1/models/{model}:countTokens` | Count Gemini v1 tokens through Kimi token estimation. |

### Kimi Utilities

| Endpoint | Description |
| --- | --- |
| `GET /v1/models` | Return the configured Kimi-compatible model list. |
| `POST /v1/tokenizers/estimate-token-count` | Forward Kimi token estimation requests. |
| `GET /health` | Health check endpoint. |

Kimi file APIs are intentionally not implemented yet.

## Mapping Notes

| Target feature | Kimi mapping |
| --- | --- |
| Kimi native chat | Use `/kimi/v1/chat/completions` for passthrough semantics. |
| OpenAI-compatible chat | Use `/v1/chat/completions` for role/tool/response compatibility and optional local `store=true`. |
| OpenAI Chat `developer` role | Converted to `system`. |
| `max_completion_tokens` / `max_output_tokens` / Anthropic `max_tokens` / Gemini `maxOutputTokens` | Forwarded as chat completion token limits. |
| Function tools | Forwarded as Kimi function tools; namespace/custom tools are flattened and restored best-effort. |
| `response_format.type=json_object` | Forwarded as Kimi JSON mode. |
| `response_format.type=json_schema` and equivalent Responses/Anthropic/Gemini schema settings | Forwarded as Kimi native Structured Output. |
| Kimi `thinking` / returned `reasoning_content` | Preserved in Chat; mapped to Responses reasoning items, Anthropic thinking blocks, and Gemini thought parts where applicable. |
| Token counting | Uses Kimi `/v1/tokenizers/estimate-token-count`; there is no local tokenizer fallback. |

OpenAI/Anthropic/Gemini hosted or built-in server tools, such as hosted web search, file search, code interpreter, computer use, and Gemini/Anthropic server-side search/code tools, are not executed by this emulator unless represented as ordinary client-executed function tools.

## License

This project is licensed under the GNU General Public License v3.0 or later (GPLv3+). See [LICENSE](LICENSE) for details.
