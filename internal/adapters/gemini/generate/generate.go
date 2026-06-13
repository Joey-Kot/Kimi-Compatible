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

package generate

import (
	"encoding/json"
	"fmt"
	"strings"

	"kimi-compatible/internal/adapters/openai/shared"
)

type Adapter struct {
	DefaultModel string
}

type Prepared struct {
	ChatPayload shared.Map
	Messages    []shared.Map
}

func (a Adapter) BuildKimiPayload(model string, payload shared.Map) (Prepared, error) {
	if model == "" {
		model = shared.StringValue(payload["model"])
	}
	if model == "" {
		model = a.DefaultModel
	}
	messages, err := NormalizeContents(payload)
	if err != nil {
		return Prepared{}, err
	}
	chatPayload := shared.Map{"model": model, "messages": messages, "stream": false}
	if generation, ok := payload["generationConfig"].(map[string]any); ok {
		MapGenerationConfig(generation, messages, chatPayload)
	}
	tools, allowedNames := NormalizeTools(payload["tools"])
	if allowedNames == nil {
		allowedNames = map[string]bool{}
	}
	toolChoice := NormalizeToolChoice(payload["toolConfig"], allowedNames)
	if len(allowedNames) > 0 {
		tools = filterTools(tools, allowedNames)
	}
	if len(tools) > 0 {
		chatPayload["tools"] = tools
	}
	if toolChoice != nil {
		chatPayload["tool_choice"] = toolChoice
	}
	return Prepared{ChatPayload: chatPayload, Messages: messages}, nil
}

func NormalizeContents(payload shared.Map) ([]shared.Map, error) {
	messages := []shared.Map{}
	if system := SystemInstructionText(payload["systemInstruction"]); system != "" {
		messages = append(messages, shared.Map{"role": "system", "content": system})
	}
	switch contents := payload["contents"].(type) {
	case string:
		messages = append(messages, shared.Map{"role": "user", "content": contents})
	case []any:
		for _, raw := range contents {
			content, ok := raw.(map[string]any)
			if !ok {
				messages = append(messages, shared.Map{"role": "user", "content": shared.StringValue(raw)})
				continue
			}
			messages = append(messages, ContentToChatMessages(content)...)
		}
	default:
		if payload["contents"] == nil {
			return nil, fmt.Errorf("'contents' is required")
		}
		messages = append(messages, shared.Map{"role": "user", "content": shared.StringValue(payload["contents"])})
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("'contents' must contain at least one content item")
	}
	return messages, nil
}

func SystemInstructionText(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case map[string]any:
		return PartsText(v["parts"])
	default:
		return shared.StringValue(value)
	}
}

func ContentToChatMessages(content map[string]any) []shared.Map {
	role := shared.StringValue(content["role"])
	if role == "model" {
		role = "assistant"
	}
	if role == "" {
		role = "user"
	}
	parts, ok := content["parts"].([]any)
	if !ok {
		return []shared.Map{{"role": role, "content": PartsText(content["parts"])}}
	}
	messages := []shared.Map{}
	textParts := []string{}
	contentParts := []any{}
	flushTextPart := func() {
		if len(textParts) > 0 {
			contentParts = append(contentParts, shared.Map{"type": "text", "text": strings.Join(textParts, "")})
			textParts = nil
		}
	}
	flushContent := func() {
		if len(contentParts) > 0 {
			flushTextPart()
			messages = append(messages, shared.Map{"role": role, "content": contentParts})
			contentParts = nil
			return
		}
		if len(textParts) > 0 {
			messages = append(messages, shared.Map{"role": role, "content": strings.Join(textParts, "")})
			textParts = nil
		}
	}
	for _, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok {
			textParts = append(textParts, shared.StringValue(raw))
			continue
		}
		if part["text"] != nil {
			textParts = append(textParts, shared.StringValue(part["text"]))
			continue
		}
		if media := KimiMediaPartFromGeminiPart(part); media != nil {
			flushTextPart()
			contentParts = append(contentParts, media)
			continue
		}
		if fc, ok := part["functionCall"].(map[string]any); ok {
			flushContent()
			messages = append(messages, shared.Map{
				"role":    "assistant",
				"content": "",
				"tool_calls": []any{shared.Map{
					"id":   valueOrDefault(fc["id"], shared.NewID("call")),
					"type": "function",
					"function": shared.Map{
						"name":      fc["name"],
						"arguments": jsonOrObject(fc["args"]),
					},
				}},
			})
			continue
		}
		if fr, ok := part["functionResponse"].(map[string]any); ok {
			flushContent()
			messages = append(messages, shared.Map{"role": "tool", "tool_call_id": fr["id"], "content": shared.JSONString(fr["response"])})
			continue
		}
		textParts = append(textParts, DescribeUnsupportedPart(part))
	}
	flushContent()
	if len(messages) == 0 {
		return []shared.Map{{"role": role, "content": ""}}
	}
	return messages
}

func KimiMediaPartFromGeminiPart(part map[string]any) shared.Map {
	if inline, ok := part["inlineData"].(map[string]any); ok {
		return KimiMediaPartFromGeminiInlineData(inline)
	}
	if inline, ok := part["inline_data"].(map[string]any); ok {
		return KimiMediaPartFromGeminiInlineData(inline)
	}
	if file, ok := part["fileData"].(map[string]any); ok {
		return KimiMediaPartFromGeminiFileData(file)
	}
	if file, ok := part["file_data"].(map[string]any); ok {
		return KimiMediaPartFromGeminiFileData(file)
	}
	return nil
}

func KimiMediaPartFromGeminiInlineData(data map[string]any) shared.Map {
	mimeType := shared.StringValue(data["mimeType"])
	if mimeType == "" {
		mimeType = shared.StringValue(data["mime_type"])
	}
	raw := shared.StringValue(data["data"])
	if mimeType == "" || raw == "" {
		return nil
	}
	return KimiMediaPartForURL(mimeType, "data:"+mimeType+";base64,"+raw)
}

func KimiMediaPartFromGeminiFileData(data map[string]any) shared.Map {
	mimeType := shared.StringValue(data["mimeType"])
	if mimeType == "" {
		mimeType = shared.StringValue(data["mime_type"])
	}
	uri := shared.StringValue(data["fileUri"])
	if uri == "" {
		uri = shared.StringValue(data["file_uri"])
	}
	if mimeType == "" || uri == "" {
		return nil
	}
	return KimiMediaPartForURL(mimeType, uri)
}

func KimiMediaPartForURL(mediaType string, url string) shared.Map {
	if strings.HasPrefix(mediaType, "video/") {
		return shared.Map{"type": "video_url", "video_url": shared.Map{"url": url}}
	}
	if strings.HasPrefix(mediaType, "image/") {
		return shared.Map{"type": "image_url", "image_url": shared.Map{"url": url}}
	}
	return nil
}

func PartsText(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		parts := []string{}
		for _, raw := range v {
			part, ok := raw.(map[string]any)
			if !ok {
				parts = append(parts, shared.StringValue(raw))
				continue
			}
			if part["text"] != nil {
				parts = append(parts, shared.StringValue(part["text"]))
			} else {
				parts = append(parts, DescribeUnsupportedPart(part))
			}
		}
		return strings.Join(parts, "")
	case map[string]any:
		if v["text"] != nil {
			return shared.StringValue(v["text"])
		}
		return shared.JSONString(v)
	default:
		return shared.StringValue(value)
	}
}

func DescribeUnsupportedPart(part map[string]any) string {
	return "\n[Gemini part omitted by Kimi compatibility layer: " + shared.JSONString(part) + "]\n"
}

func MapGenerationConfig(config map[string]any, messages []shared.Map, chatPayload shared.Map) {
	for source, target := range map[string]string{
		"maxOutputTokens":    "max_tokens",
		"temperature":        "temperature",
		"topP":               "top_p",
		"stopSequences":      "stop",
		"presencePenalty":    "presence_penalty",
		"frequencyPenalty":   "frequency_penalty",
		"candidateCount":     "n",
		"responseLogprobs":   "logprobs",
		"logprobs":           "top_logprobs",
		"seed":               "seed",
		"responseModalities": "modalities",
	} {
		if value, ok := config[source]; ok && value != nil {
			chatPayload[target] = value
		}
	}
	if mime := shared.StringValue(config["responseMimeType"]); mime == "application/json" {
		chatPayload["response_format"] = shared.Map{"type": "json_object"}
	}
	if schema := config["responseSchema"]; schema != nil {
		chatPayload["response_format"] = shared.Map{
			"type": "json_schema",
			"json_schema": shared.Map{
				"name":   "response",
				"schema": schema,
			},
		}
	}
	if thinking, ok := config["thinkingConfig"].(map[string]any); ok {
		MapThinking(thinking, chatPayload)
	}
}

func MapThinking(thinking map[string]any, chatPayload shared.Map) {
	if include, ok := thinking["includeThoughts"].(bool); ok && include {
		chatPayload["thinking"] = shared.Map{"type": "enabled"}
	}
	if budget := shared.IntValue(thinking["thinkingBudget"], 0); budget > 0 {
		chatPayload["thinking"] = shared.Map{"type": "enabled"}
		if budget >= 32000 {
			chatPayload["reasoning_effort"] = "max"
		} else {
			chatPayload["reasoning_effort"] = "high"
		}
	}
	switch strings.ToLower(shared.StringValue(thinking["thinkingLevel"])) {
	case "low", "medium", "high":
		chatPayload["thinking"] = shared.Map{"type": "enabled"}
		chatPayload["reasoning_effort"] = "high"
	case "none":
		chatPayload["thinking"] = shared.Map{"type": "disabled"}
	}
}

func NormalizeTools(value any) ([]shared.Map, map[string]bool) {
	rawTools, ok := value.([]any)
	if !ok {
		return nil, nil
	}
	tools := []shared.Map{}
	allowed := map[string]bool{}
	for _, raw := range rawTools {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if declarations, ok := tool["functionDeclarations"].([]any); ok {
			for _, rawDecl := range declarations {
				decl, ok := rawDecl.(map[string]any)
				if !ok {
					continue
				}
				name := shared.StringValue(decl["name"])
				if name == "" {
					continue
				}
				function := shared.Map{
					"name":        shared.SafeToolName(name),
					"description": decl["description"],
					"parameters":  valueOrDefault(decl["parameters"], shared.Map{}),
				}
				if decl["response"] != nil {
					function["description"] = strings.TrimSpace(shared.StringValue(function["description"]) + "\nExpected function response schema: " + shared.JSONString(decl["response"]))
				}
				tools = append(tools, shared.Map{"type": "function", "function": function})
			}
		}
	}
	return tools, allowed
}

func NormalizeToolChoice(value any, allowed map[string]bool) any {
	toolConfig, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	fc, ok := toolConfig["functionCallingConfig"].(map[string]any)
	if !ok {
		return nil
	}
	for _, rawName := range namesList(fc["allowedFunctionNames"]) {
		allowed[shared.SafeToolName(rawName)] = true
	}
	mode := strings.ToUpper(shared.StringValue(fc["mode"]))
	switch mode {
	case "NONE":
		return "none"
	case "ANY":
		if len(allowed) == 1 {
			for name := range allowed {
				return shared.Map{"type": "function", "function": shared.Map{"name": name}}
			}
		}
		return "required"
	case "AUTO", "VALIDATED", "":
		return "auto"
	default:
		return nil
	}
}

func ResponseFromKimi(completion shared.Map, model string) shared.Map {
	if model == "" {
		model = shared.StringValue(completion["model"])
	}
	choices, _ := completion["choices"].([]any)
	candidates := []any{}
	for index, raw := range choices {
		choice, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		message, _ := choice["message"].(map[string]any)
		candidates = append(candidates, shared.Map{
			"index":        index,
			"content":      ContentFromMessage(message),
			"finishReason": FinishReason(shared.StringValue(choice["finish_reason"])),
		})
	}
	if len(candidates) == 0 {
		candidates = append(candidates, shared.Map{"index": 0, "content": shared.Map{"role": "model", "parts": []any{shared.Map{"text": ""}}}, "finishReason": "STOP"})
	}
	return shared.Map{
		"candidates":    candidates,
		"usageMetadata": UsageFromKimi(completion["usage"]),
		"modelVersion":  model,
		"responseId":    valueOrDefault(completion["id"], shared.NewID("gemini")),
	}
}

func ContentFromMessage(message map[string]any) shared.Map {
	parts := []any{}
	if reasoning := shared.StringValue(message["reasoning_content"]); reasoning != "" {
		parts = append(parts, shared.Map{"text": reasoning, "thought": true})
	}
	if text := shared.ContentToText(message["content"], false); text != "" {
		parts = append(parts, shared.Map{"text": text})
	}
	if calls, ok := message["tool_calls"].([]any); ok {
		for _, raw := range calls {
			call, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			function, _ := call["function"].(map[string]any)
			args := shared.Map{}
			if rawArgs := shared.StringValue(function["arguments"]); rawArgs != "" {
				var parsed map[string]any
				if err := json.Unmarshal([]byte(rawArgs), &parsed); err == nil {
					args = parsed
				} else {
					args = shared.Map{"input": rawArgs}
				}
			}
			parts = append(parts, shared.Map{"functionCall": shared.Map{"id": call["id"], "name": function["name"], "args": args}})
		}
	}
	if len(parts) == 0 {
		parts = append(parts, shared.Map{"text": ""})
	}
	return shared.Map{"role": "model", "parts": parts}
}

func FinishReason(reason string) string {
	switch reason {
	case "stop", "":
		return "STOP"
	case "length":
		return "MAX_TOKENS"
	case "tool_calls":
		return "STOP"
	case "content_filter":
		return "SAFETY"
	default:
		return strings.ToUpper(reason)
	}
}

func UsageFromKimi(value any) shared.Map {
	usage, ok := value.(map[string]any)
	if !ok {
		return shared.Map{"promptTokenCount": 0, "candidatesTokenCount": 0, "totalTokenCount": 0}
	}
	prompt := shared.IntValue(usage["prompt_tokens"], 0)
	candidates := shared.IntValue(usage["completion_tokens"], 0)
	total := shared.IntValue(usage["total_tokens"], prompt+candidates)
	out := shared.Map{"promptTokenCount": prompt, "candidatesTokenCount": candidates, "totalTokenCount": total}
	if thoughts := shared.IntValue(usage["reasoning_tokens"], 0); thoughts > 0 {
		out["thoughtsTokenCount"] = thoughts
	}
	return out
}

func StreamChunkFromKimi(chunk shared.Map, model string) shared.Map {
	converted := shared.CloneMap(chunk)
	if choices, ok := converted["choices"].([]any); ok {
		newChoices := make([]any, 0, len(choices))
		for _, raw := range choices {
			choice, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			c := shared.CloneMap(choice)
			if c["message"] == nil && c["delta"] != nil {
				c["message"] = c["delta"]
			}
			newChoices = append(newChoices, c)
		}
		converted["choices"] = newChoices
	}
	return ResponseFromKimi(converted, model)
}

func CountTokensResponse(totalTokens int) shared.Map {
	return shared.Map{"totalTokens": totalTokens}
}

func namesList(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := []string{}
	for _, item := range raw {
		if text := shared.StringValue(item); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func filterTools(tools []shared.Map, allowed map[string]bool) []shared.Map {
	if len(allowed) == 0 {
		return tools
	}
	out := []shared.Map{}
	for _, tool := range tools {
		if fn, ok := tool["function"].(map[string]any); ok && allowed[shared.StringValue(fn["name"])] {
			out = append(out, tool)
		}
	}
	return out
}

func jsonOrObject(value any) string {
	if value == nil {
		return "{}"
	}
	return shared.JSONString(value)
}

func valueOrDefault(value any, fallback any) any {
	if value == nil {
		return fallback
	}
	return value
}
