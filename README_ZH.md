# Kimi 多协议仿真器

[English](README.md) | 中文

这是一个面向 Kimi 的协议仿真器。它对外提供 Kimi 原生透传命名空间，以及 OpenAI Chat Completions、OpenAI Responses、Anthropic Messages 和 Gemini Generate Content 等兼容 API，并在 Kimi Chat Completions 之上尽可能高保真地仿真这些协议的请求与响应语义。

本地运行时通过命令行参数进行配置，容器部署时通过环境变量进行配置。

## 配置与使用

本地运行：

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

容器运行：

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

容器环境变量：

| 环境变量 | 等价 flag |
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

## 兼容端点

### Kimi 原生 Chat Completions

| 端点 | 说明 |
| --- | --- |
| `POST /kimi/v1/chat/completions` | Kimi 原生 Chat Completions 透传入口。 |

### OpenAI Chat Completions

| 端点 | 说明 |
| --- | --- |
| `POST /v1/chat/completions` | 创建 OpenAI 兼容 Chat Completions，经兼容适配器转发到 Kimi Chat Completions。 |
| `GET /v1/chat/completions` | 列出本地保存的 Chat Completions。 |
| `GET /v1/chat/completions/{completion_id}` | 读取本地保存的单个 Chat Completion。 |
| `POST /v1/chat/completions/{completion_id}` | 更新本地保存的 Chat Completion 元数据。 |
| `DELETE /v1/chat/completions/{completion_id}` | 删除本地保存的 Chat Completion。 |
| `GET /v1/chat/completions/{completion_id}/messages` | 列出本地保存的 Chat Completion 消息。 |

### OpenAI Responses

| 端点 | 说明 |
| --- | --- |
| `POST /v1/responses` | 创建 Responses，并转发到 Kimi Chat Completions。 |
| `GET /v1/responses/{response_id}` | 读取本地保存的 Response。 |
| `DELETE /v1/responses/{response_id}` | 删除本地保存的 Response。 |
| `GET /v1/responses/{response_id}/input_items` | 列出本地保存的 Response 输入项。 |
| `POST /v1/responses/{response_id}/cancel` | 按本地状态语义取消 Response。 |
| `POST /v1/responses/input_tokens` | 通过 Kimi `/v1/tokenizers/estimate-token-count` 统计输入 token。 |
| `POST /v1/responses/compact` | 使用 Kimi 做尽力而为的上下文压缩总结。 |

### OpenAI Conversations

| 端点 | 说明 |
| --- | --- |
| `POST /v1/conversations` | 创建本地 Conversation。 |
| `GET /v1/conversations/{conversation_id}` | 读取本地 Conversation。 |
| `POST /v1/conversations/{conversation_id}` | 追加或更新本地 Conversation。 |
| `DELETE /v1/conversations/{conversation_id}` | 删除本地 Conversation。 |

### Anthropic Messages

| 端点 | 说明 |
| --- | --- |
| `POST /v1/messages` | 创建 Anthropic Messages 响应，并转发到 Kimi Chat Completions。 |
| `POST /v1/messages/count_tokens` | 通过 Kimi token estimate 统计 Anthropic Messages token。 |

### Gemini Generate Content

| 端点 | 说明 |
| --- | --- |
| `POST /v1beta/models/{model}:generateContent` | 创建 Gemini Generate Content 响应，并转发到 Kimi Chat Completions。 |
| `POST /v1beta/models/{model}:streamGenerateContent` | 创建流式 Gemini Generate Content 响应。 |
| `POST /v1beta/models/{model}:countTokens` | 通过 Kimi token estimate 统计 Gemini v1beta token。 |
| `POST /v1/models/{model}:generateContent` | 创建 Gemini v1 Generate Content 响应，并转发到 Kimi Chat Completions。 |
| `POST /v1/models/{model}:streamGenerateContent` | 创建流式 Gemini v1 Generate Content 响应。 |
| `POST /v1/models/{model}:countTokens` | 通过 Kimi token estimate 统计 Gemini v1 token。 |

### Kimi 工具端点

| 端点 | 说明 |
| --- | --- |
| `GET /v1/models` | 返回配置的 Kimi 兼容模型列表。 |
| `POST /v1/tokenizers/estimate-token-count` | 转发 Kimi token estimate 请求。 |
| `GET /health` | 健康检查。 |

Kimi 文件 API 暂未实现。

## 映射说明

| 目标特性 | Kimi 映射 |
| --- | --- |
| Kimi 原生聊天 | 使用 `/kimi/v1/chat/completions` 获取透传语义。 |
| OpenAI 兼容聊天 | 使用 `/v1/chat/completions` 获取 role/tool/response 兼容和可选本地 `store=true`。 |
| OpenAI Chat `developer` role | 转为 `system`。 |
| `max_completion_tokens` / `max_output_tokens` / Anthropic `max_tokens` / Gemini `maxOutputTokens` | 映射为聊天补全输出 token 限制。 |
| Function tools | 转发为 Kimi function tools；namespace/custom tools 会展平成 function tools，返回时尽力还原。 |
| `response_format.type=json_object` | 转发为 Kimi JSON mode。 |
| `response_format.type=json_schema` 以及 Responses/Anthropic/Gemini 的等价 schema 设置 | 转发为 Kimi 原生 Structured Output。 |
| Kimi `thinking` / 返回的 `reasoning_content` | Chat 中保留；在 Responses、Anthropic、Gemini 中映射到各自的 reasoning/thinking/thought 结构。 |
| Token 计算 | 使用 Kimi `/v1/tokenizers/estimate-token-count`；没有本地 tokenizer fallback。 |

OpenAI/Anthropic/Gemini 的 hosted 或 built-in server tools，例如 hosted web search、file search、code interpreter、computer use，以及 Gemini/Anthropic 服务端 search/code 工具，不由本仿真器执行；除非它们被表达为普通的客户端执行 function tools。

## License

本项目基于 GNU General Public License v3.0 or later（GPLv3+）授权。详情请查看 [LICENSE](LICENSE)。
