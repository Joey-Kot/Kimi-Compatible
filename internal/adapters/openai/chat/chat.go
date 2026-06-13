// Copyright (C) 2026 Joey Kot <joey.kot.x@gmail.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed WITHOUT ANY WARRANTY; without even the
// implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.
// See <https://www.gnu.org/licenses/> for more details.

package chat

import (
	"fmt"

	"kimi-compatible/internal/adapters/openai/shared"
)

type Adapter struct {
	DefaultModel string
}

func (a Adapter) BuildKimiPayload(payload shared.Map) (shared.Map, []shared.Map, error) {
	messages, err := NormalizeMessages(payload["messages"])
	if err != nil {
		return nil, nil, err
	}
	model := shared.StringValue(payload["model"])
	if model == "" {
		model = a.DefaultModel
	}
	chatPayload := shared.Map{
		"model":    model,
		"messages": messages,
		"stream":   shared.BoolValue(payload["stream"]),
	}

	fieldMap := map[string]string{
		"temperature":         "temperature",
		"top_p":               "top_p",
		"stop":                "stop",
		"presence_penalty":    "presence_penalty",
		"frequency_penalty":   "frequency_penalty",
		"logprobs":            "logprobs",
		"top_logprobs":        "top_logprobs",
		"n":                   "n",
		"seed":                "seed",
		"user":                "user",
		"logit_bias":          "logit_bias",
		"parallel_tool_calls": "parallel_tool_calls",
		"stream_options":      "stream_options",
		"thinking":            "thinking",
		"reasoning_effort":    "reasoning_effort",
	}
	for source, target := range fieldMap {
		if value, ok := payload[source]; ok && value != nil {
			chatPayload[target] = value
		}
	}
	if value, ok := payload["max_completion_tokens"]; ok && value != nil {
		chatPayload["max_tokens"] = value
	} else if value, ok := payload["max_tokens"]; ok && value != nil {
		chatPayload["max_tokens"] = value
	}

	MapReasoning(payload, chatPayload)

	tools := NormalizeTools(payload)
	if choice, ok := payload["tool_choice"].(map[string]any); ok && shared.StringValue(choice["type"]) == "allowed_tools" {
		tools = filterAllowedTools(tools, choice)
	}
	if len(tools) > 0 {
		chatPayload["tools"] = tools
	}
	if mapped := NormalizeToolChoice(payload["tool_choice"], payload["function_call"]); mapped != nil {
		chatPayload["tool_choice"] = mapped
	}

	messages = ApplyResponseFormat(payload, messages, chatPayload)
	return chatPayload, messages, nil
}

func NormalizeMessages(value any) ([]shared.Map, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("'messages' must be an array")
	}
	out := make([]shared.Map, 0, len(items))
	for _, item := range items {
		message, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("each chat message must be an object")
		}
		normalized := shared.CloneMap(message)
		role := shared.StringValue(normalized["role"])
		switch role {
		case "developer":
			normalized["role"] = "system"
		case "function":
			normalized["role"] = "tool"
			if _, ok := normalized["tool_call_id"]; !ok {
				name := shared.StringValue(normalized["name"])
				if name == "" {
					name = shared.NewID("call")
				}
				normalized["tool_call_id"] = name
			}
		case "system", "user", "assistant", "tool":
		default:
			return nil, fmt.Errorf("unsupported chat message role")
		}
		if content, ok := normalized["content"]; ok {
			normalized["content"] = normalizeContent(content)
		}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("'messages' must contain at least one message")
	}
	return out, nil
}

func normalizeContent(value any) any {
	switch value.(type) {
	case nil, string, []any:
		return value
	default:
		return shared.StringValue(value)
	}
}

func NormalizeFunctionTool(function map[string]any) shared.Map {
	if function == nil || shared.StringValue(function["name"]) == "" {
		return nil
	}
	normalized := shared.CloneMap(function)
	normalized["name"] = shared.SafeToolName(shared.StringValue(normalized["name"]))
	if _, ok := normalized["parameters"]; !ok || normalized["parameters"] == nil {
		normalized["parameters"] = shared.Map{}
	}
	return shared.Map{"type": "function", "function": normalized}
}

func NormalizeTool(tool any) shared.Map {
	t, ok := tool.(map[string]any)
	if !ok {
		return nil
	}
	switch shared.StringValue(t["type"]) {
	case "function":
		function, ok := t["function"].(map[string]any)
		if !ok {
			return nil
		}
		return NormalizeFunctionTool(function)
	case "custom":
		custom, ok := t["custom"].(map[string]any)
		if !ok || shared.StringValue(custom["name"]) == "" {
			return nil
		}
		return shared.Map{
			"type": "function",
			"function": shared.Map{
				"name":        shared.SafeToolName(shared.StringValue(custom["name"])),
				"description": ToolDescription(custom["description"], "", "", custom["format"]),
				"parameters": shared.Map{
					"type": "object",
					"properties": shared.Map{
						"input": shared.Map{"type": "string", "description": "Raw input for this custom tool."},
					},
					"required":             []any{"input"},
					"additionalProperties": false,
				},
			},
		}
	default:
		return nil
	}
}

func NormalizeTools(payload shared.Map) []shared.Map {
	tools := []shared.Map{}
	if rawTools, ok := payload["tools"].([]any); ok {
		for _, tool := range rawTools {
			if normalized := NormalizeTool(tool); normalized != nil {
				tools = append(tools, normalized)
			}
		}
	}
	if rawFunctions, ok := payload["functions"].([]any); ok {
		existing := map[string]bool{}
		for _, tool := range tools {
			if function, ok := tool["function"].(map[string]any); ok {
				existing[shared.StringValue(function["name"])] = true
			}
		}
		for _, function := range rawFunctions {
			fn, ok := function.(map[string]any)
			if !ok {
				continue
			}
			normalized := NormalizeFunctionTool(fn)
			if normalized == nil {
				continue
			}
			name := shared.StringValue(normalized["function"].(map[string]any)["name"])
			if !existing[name] {
				tools = append(tools, normalized)
				existing[name] = true
			}
		}
	}
	return tools
}

func NormalizeToolChoice(toolChoice any, functionCall any) any {
	if text, ok := toolChoice.(string); ok {
		if text == "none" || text == "auto" || text == "required" {
			return text
		}
	}
	if choice, ok := toolChoice.(map[string]any); ok {
		switch shared.StringValue(choice["type"]) {
		case "function":
			if function, ok := choice["function"].(map[string]any); ok {
				if name := shared.StringValue(function["name"]); name != "" {
					return shared.Map{"type": "function", "function": shared.Map{"name": shared.SafeToolName(name)}}
				}
			}
		case "custom":
			if custom, ok := choice["custom"].(map[string]any); ok {
				if name := shared.StringValue(custom["name"]); name != "" {
					return shared.Map{"type": "function", "function": shared.Map{"name": shared.SafeToolName(name)}}
				}
			}
		case "allowed_tools":
			if allowed, ok := choice["allowed_tools"].(map[string]any); ok {
				mode := shared.StringValue(allowed["mode"])
				if mode == "auto" || mode == "required" {
					return mode
				}
			}
		}
	}
	if text, ok := functionCall.(string); ok && (text == "none" || text == "auto") {
		return text
	}
	if fn, ok := functionCall.(map[string]any); ok {
		if name := shared.StringValue(fn["name"]); name != "" {
			return shared.Map{"type": "function", "function": shared.Map{"name": shared.SafeToolName(name)}}
		}
	}
	return nil
}

func MapReasoning(payload shared.Map, chatPayload shared.Map) {
	if reasoning, ok := payload["reasoning"].(map[string]any); ok {
		effort := shared.StringValue(reasoning["effort"])
		switch effort {
		case "none", "minimal":
			chatPayload["thinking"] = shared.Map{"type": "disabled"}
		case "low", "medium", "high":
			chatPayload["thinking"] = shared.Map{"type": "enabled"}
			chatPayload["reasoning_effort"] = "high"
		case "xhigh":
			chatPayload["thinking"] = shared.Map{"type": "enabled"}
			chatPayload["reasoning_effort"] = "max"
		}
		return
	}
	switch shared.StringValue(payload["reasoning_effort"]) {
	case "low", "medium":
		chatPayload["reasoning_effort"] = "high"
	case "xhigh":
		chatPayload["reasoning_effort"] = "max"
	}
}

func ApplyResponseFormat(payload shared.Map, messages []shared.Map, chatPayload shared.Map) []shared.Map {
	responseFormat, ok := payload["response_format"].(map[string]any)
	if !ok {
		return messages
	}
	switch shared.StringValue(responseFormat["type"]) {
	case "json_object":
		chatPayload["response_format"] = shared.Map{"type": "json_object"}
	case "json_schema":
		chatPayload["response_format"] = KimiJSONSchemaResponseFormat(responseFormat, "response")
	}
	return messages
}

func KimiJSONSchemaResponseFormat(formatConfig map[string]any, fallbackName string) shared.Map {
	jsonSchema, ok := formatConfig["json_schema"].(map[string]any)
	if !ok {
		jsonSchema = formatConfig
	}
	normalized := shared.CloneMap(jsonSchema)
	if shared.StringValue(normalized["name"]) == "" {
		normalized["name"] = fallbackName
	}
	if normalized["schema"] == nil {
		normalized["schema"] = shared.Map{}
	}
	return shared.Map{"type": "json_schema", "json_schema": normalized}
}

func JSONSchemaInstruction(formatConfig map[string]any) string {
	if shared.StringValue(formatConfig["type"]) != "json_schema" {
		return ""
	}
	name := shared.StringValue(formatConfig["name"])
	if name == "" {
		name = "response"
	}
	schema := formatConfig["schema"]
	if schema == nil {
		schema = shared.Map{}
	}
	text := "Return a valid JSON object matching the JSON Schema named " + name + "."
	if description := shared.StringValue(formatConfig["description"]); description != "" {
		text += "\n" + description
	}
	text += "\nJSON Schema: " + shared.JSONString(schema)
	return text
}

func ChatJSONSchemaInstruction(responseFormat map[string]any) string {
	jsonSchema, ok := responseFormat["json_schema"].(map[string]any)
	if !ok {
		return ""
	}
	return JSONSchemaInstruction(map[string]any{
		"type":        "json_schema",
		"name":        jsonSchema["name"],
		"description": jsonSchema["description"],
		"schema":      jsonSchema["schema"],
	})
}

func ToolDescription(description any, namespace, namespaceDescription string, customFormat any) string {
	parts := []string{}
	if namespace != "" {
		parts = append(parts, "Namespace: "+namespace+".")
	}
	if namespaceDescription != "" {
		parts = append(parts, namespaceDescription)
	}
	if text := shared.StringValue(description); text != "" {
		parts = append(parts, text)
	}
	if customFormat != nil {
		parts = append(parts, "Custom tool input format: "+shared.JSONString(customFormat))
	}
	return joinLines(parts)
}

func CompletionFromKimi(completion shared.Map, requestPayload shared.Map, defaultModel string) shared.Map {
	created := int64(shared.IntValue(completion["created"], int(shared.NowSeconds())))
	completionID := shared.StringValue(completion["id"])
	if completionID == "" {
		completionID = shared.NewID("chatcmpl")
	}
	choices := []any{}
	if rawChoices, ok := completion["choices"].([]any); ok {
		for index, raw := range rawChoices {
			choice, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			c := shared.CloneMap(choice)
			if _, ok := c["index"]; !ok || c["index"] == nil {
				c["index"] = index
			}
			c["message"] = NormalizeCompletionMessage(c["message"])
			if _, ok := c["finish_reason"]; !ok {
				c["finish_reason"] = nil
			}
			if _, ok := c["logprobs"]; !ok {
				c["logprobs"] = nil
			}
			choices = append(choices, c)
		}
	}
	model := shared.StringValue(completion["model"])
	if model == "" {
		model = shared.StringValue(requestPayload["model"])
	}
	if model == "" {
		model = defaultModel
	}
	return shared.Map{
		"id":                 completionID,
		"object":             "chat.completion",
		"created":            created,
		"model":              model,
		"choices":            choices,
		"usage":              UsageFromKimi(completion["usage"]),
		"metadata":           metadataOrEmpty(requestPayload["metadata"]),
		"store":              shared.BoolValue(requestPayload["store"]),
		"service_tier":       valueOrDefault(requestPayload["service_tier"], "auto"),
		"system_fingerprint": completion["system_fingerprint"],
	}
}

func NormalizeCompletionMessage(message any) shared.Map {
	normalized, ok := message.(map[string]any)
	if !ok {
		normalized = shared.Map{}
	}
	out := shared.CloneMap(normalized)
	if shared.StringValue(out["role"]) == "" {
		out["role"] = "assistant"
	}
	if _, ok := out["content"]; !ok {
		out["content"] = ""
	}
	if calls, ok := out["tool_calls"].([]any); ok && len(calls) == 0 {
		delete(out, "tool_calls")
	}
	return out
}

func UsageFromKimi(value any) any {
	usage, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := shared.CloneMap(usage)
	promptTokens := shared.IntValue(out["prompt_tokens"], 0)
	completionTokens := shared.IntValue(out["completion_tokens"], 0)
	out["prompt_tokens"] = promptTokens
	out["completion_tokens"] = completionTokens
	out["total_tokens"] = shared.IntValue(out["total_tokens"], promptTokens+completionTokens)

	promptDetails, _ := out["prompt_tokens_details"].(map[string]any)
	if promptDetails == nil {
		promptDetails = shared.Map{}
	}
	if v, ok := out["prompt_cache_hit_tokens"]; ok {
		promptDetails["cached_tokens"] = shared.IntValue(v, 0)
	}
	out["prompt_tokens_details"] = promptDetails

	completionDetails, _ := out["completion_tokens_details"].(map[string]any)
	if completionDetails == nil {
		completionDetails = shared.Map{}
	}
	if v, ok := out["reasoning_tokens"]; ok {
		completionDetails["reasoning_tokens"] = shared.IntValue(v, 0)
	}
	out["completion_tokens_details"] = completionDetails
	return out
}

func SSEData(chunk shared.Map, requestPayload shared.Map, defaultModel string) shared.Map {
	data := shared.CloneMap(chunk)
	if shared.StringValue(data["id"]) == "" {
		data["id"] = shared.NewID("chatcmpl")
	}
	data["object"] = "chat.completion.chunk"
	if _, ok := data["created"]; !ok || data["created"] == nil {
		data["created"] = shared.NowSeconds()
	}
	model := shared.StringValue(data["model"])
	if model == "" {
		model = shared.StringValue(requestPayload["model"])
	}
	if model == "" {
		model = defaultModel
	}
	data["model"] = model
	if _, ok := data["service_tier"]; !ok {
		data["service_tier"] = valueOrDefault(requestPayload["service_tier"], "auto")
	}
	if _, ok := data["system_fingerprint"]; !ok {
		data["system_fingerprint"] = nil
	}
	if data["usage"] != nil {
		data["usage"] = UsageFromKimi(data["usage"])
	}
	return data
}

func StoredMessages(requestMessages []shared.Map, completion shared.Map, completionID string) []shared.Map {
	out := []shared.Map{}
	for index, message := range requestMessages {
		out = append(out, StoredMessage(message, index, completionID))
	}
	if choices, ok := completion["choices"].([]any); ok {
		for _, rawChoice := range choices {
			choice, ok := rawChoice.(map[string]any)
			if !ok {
				continue
			}
			if message, ok := choice["message"].(map[string]any); ok {
				out = append(out, StoredMessage(message, len(out), completionID))
			}
		}
	}
	return out
}

func StoredMessage(message shared.Map, index int, completionID string) shared.Map {
	stored := shared.CloneMap(message)
	if shared.StringValue(stored["id"]) == "" {
		stored["id"] = fmt.Sprintf("%s-%d", completionID, index)
	}
	if _, ok := stored["content"].([]any); ok {
		stored["content_parts"] = stored["content"]
	} else {
		stored["content_parts"] = nil
	}
	if _, ok := stored["name"]; !ok {
		stored["name"] = nil
	}
	return stored
}

func filterAllowedTools(tools []shared.Map, choice map[string]any) []shared.Map {
	allowed, ok := choice["allowed_tools"].(map[string]any)
	if !ok {
		return tools
	}
	raw, ok := allowed["tools"].([]any)
	if !ok {
		return tools
	}
	allowedNames := map[string]bool{}
	for _, entry := range raw {
		e, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		name := ""
		if fn, ok := e["function"].(map[string]any); ok {
			name = shared.StringValue(fn["name"])
		}
		if custom, ok := e["custom"].(map[string]any); ok && name == "" {
			name = shared.StringValue(custom["name"])
		}
		if name != "" {
			allowedNames[shared.SafeToolName(name)] = true
		}
	}
	if len(allowedNames) == 0 {
		return tools
	}
	out := []shared.Map{}
	for _, tool := range tools {
		if fn, ok := tool["function"].(map[string]any); ok && allowedNames[shared.StringValue(fn["name"])] {
			out = append(out, tool)
		}
	}
	return out
}

func valueOrDefault(value any, fallback any) any {
	if value == nil {
		return fallback
	}
	return value
}

func metadataOrEmpty(value any) any {
	if value == nil {
		return shared.Map{}
	}
	return value
}

func joinLines(parts []string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += "\n"
		}
		out += part
	}
	return out
}
