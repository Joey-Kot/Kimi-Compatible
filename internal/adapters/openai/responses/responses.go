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

package responses

import (
	"encoding/json"
	"fmt"
	"strings"

	"kimi-compatible/internal/adapters/openai/chat"
	"kimi-compatible/internal/adapters/openai/shared"
	"kimi-compatible/internal/state"
)

type Adapter struct {
	DefaultModel string
	Store        *state.Store
}

type Prepared struct {
	Messages       []shared.Map
	AllItems       []shared.Map
	InputItems     []shared.Map
	ConversationID string
}

func (a Adapter) Prepare(payload shared.Map) (Prepared, error) {
	contextItems, conversationID, err := a.contextItemsForPayload(payload)
	if err != nil {
		return Prepared{}, err
	}
	inputItems, err := a.NormalizeInputItems(payload["input"])
	if err != nil {
		return Prepared{}, err
	}
	allItems := append(shared.CloneSlice(contextItems), inputItems...)
	messages := InputItemsToChatMessages(allItems)
	if instructions := shared.StringValue(payload["instructions"]); instructions != "" {
		messages = append([]shared.Map{{"role": "system", "content": instructions}}, messages...)
	}
	if len(messages) == 0 {
		return Prepared{}, fmt.Errorf("no text input or conversation context was provided")
	}
	return Prepared{Messages: messages, AllItems: allItems, InputItems: inputItems, ConversationID: conversationID}, nil
}

func (a Adapter) contextItemsForPayload(payload shared.Map) ([]shared.Map, string, error) {
	conversationID, err := ConversationIDFromParam(payload["conversation"])
	if err != nil {
		return nil, "", err
	}
	previousID := shared.StringValue(payload["previous_response_id"])
	if conversationID != "" && previousID != "" {
		return nil, "", fmt.Errorf("'conversation' cannot be used with 'previous_response_id'")
	}
	if a.Store == nil {
		return nil, "", nil
	}
	if conversationID != "" {
		items, ok := a.Store.ConversationItemsFor(conversationID)
		if !ok {
			return nil, "", fmt.Errorf("conversation not found: %s", conversationID)
		}
		return items, conversationID, nil
	}
	if previousID != "" {
		if _, ok := a.Store.Response(previousID); !ok {
			return nil, "", fmt.Errorf("response not found: %s", previousID)
		}
		items, _ := a.Store.ResponseContext(previousID)
		return items, "", nil
	}
	return nil, "", nil
}

func ConversationIDFromParam(value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case map[string]any:
		if id := shared.StringValue(v["id"]); id != "" {
			return id, nil
		}
	}
	return "", fmt.Errorf("'conversation' must be a string or an object with id")
}

func (a Adapter) NormalizeInputItems(value any) ([]shared.Map, error) {
	if value == nil {
		return []shared.Map{}, nil
	}
	if text, ok := value.(string); ok {
		return []shared.Map{NormalizeMessageItem(shared.Map{"role": "user", "content": text})}, nil
	}
	rawItems, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("'input' must be a string or an array")
	}
	items := []shared.Map{}
	for _, raw := range rawItems {
		if text, ok := raw.(string); ok {
			items = append(items, NormalizeMessageItem(shared.Map{"role": "user", "content": text}))
			continue
		}
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		itemType := shared.StringValue(item["type"])
		if itemType == "" {
			itemType = "message"
		}
		switch itemType {
		case "item_reference":
			refID := shared.StringValue(item["id"])
			if a.Store == nil {
				return nil, fmt.Errorf("unknown item_reference id: %s", refID)
			}
			ref, ok := a.Store.Item(refID)
			if !ok {
				return nil, fmt.Errorf("unknown item_reference id: %s", refID)
			}
			items = append(items, ref)
		case "message":
			items = append(items, NormalizeMessageItem(item))
		case "function_call_output":
			items = append(items, a.NormalizeFunctionCallOutput(item, "function_call_output"))
		case "custom_tool_call_output":
			items = append(items, a.NormalizeFunctionCallOutput(item, "custom_tool_call_output"))
		case "function_call":
			items = append(items, NormalizeFunctionCall(item, "function_call"))
		case "custom_tool_call":
			items = append(items, NormalizeCustomToolCall(item))
		default:
			copied := shared.CloneMap(item)
			if shared.StringValue(copied["id"]) == "" {
				copied["id"] = shared.NewID("item")
			}
			if shared.StringValue(copied["status"]) == "" {
				copied["status"] = "completed"
			}
			items = append(items, copied)
		}
	}
	return items, nil
}

func (a Adapter) NormalizeFunctionCallOutput(item map[string]any, typ string) shared.Map {
	normalized := NormalizeFunctionCallOutput(item, typ)
	if a.Store == nil {
		return normalized
	}
	refID := shared.StringValue(normalized["call_id"])
	if refID == "" {
		return normalized
	}
	ref, ok := a.Store.Item(refID)
	if !ok || !IsToolCallItem(ref) {
		return normalized
	}
	if callID := shared.StringValue(ref["call_id"]); callID != "" {
		normalized["call_id"] = callID
	}
	return normalized
}

func NormalizeMessageItem(item map[string]any) shared.Map {
	normalized := shared.CloneMap(item)
	if shared.StringValue(normalized["type"]) == "" {
		normalized["type"] = "message"
	}
	if shared.StringValue(normalized["id"]) == "" {
		normalized["id"] = shared.NewID("msg")
	}
	if shared.StringValue(normalized["status"]) == "" {
		normalized["status"] = "completed"
	}
	role := shared.StringValue(normalized["role"])
	if role == "" {
		role = "user"
	}
	normalized["role"] = role
	content := normalized["content"]
	if text, ok := content.(string); ok {
		normalized["content"] = shared.AsMessageContent(text, role == "assistant")
	} else if rawParts, ok := content.([]any); ok {
		parts := []any{}
		for _, rawPart := range rawParts {
			if part, ok := rawPart.(map[string]any); ok {
				parts = append(parts, part)
			} else if role == "assistant" {
				parts = append(parts, shared.Map{"type": "output_text", "text": shared.StringValue(rawPart), "annotations": []any{}})
			} else {
				parts = append(parts, shared.Map{"type": "input_text", "text": shared.StringValue(rawPart)})
			}
		}
		normalized["content"] = parts
	} else {
		normalized["content"] = shared.AsMessageContent(shared.StringValue(content), role == "assistant")
	}
	return normalized
}

func NormalizeFunctionCallOutput(item map[string]any, typ string) shared.Map {
	normalized := shared.CloneMap(item)
	if shared.StringValue(normalized["id"]) == "" {
		normalized["id"] = shared.NewID("fco")
	}
	normalized["type"] = typ
	if shared.StringValue(normalized["status"]) == "" {
		normalized["status"] = "completed"
	}
	normalized["output"] = shared.ContentToText(normalized["output"], false)
	return normalized
}

func NormalizeFunctionCall(item map[string]any, typ string) shared.Map {
	normalized := shared.CloneMap(item)
	if shared.StringValue(normalized["id"]) == "" {
		normalized["id"] = shared.NewID("fc")
	}
	normalized["type"] = typ
	if shared.StringValue(normalized["status"]) == "" {
		normalized["status"] = "completed"
	}
	if shared.StringValue(normalized["call_id"]) == "" {
		normalized["call_id"] = normalized["id"]
	}
	normalized["arguments"] = shared.JSONString(normalized["arguments"])
	if normalized["arguments"] == "null" {
		normalized["arguments"] = ""
	}
	return normalized
}

func NormalizeCustomToolCall(item map[string]any) shared.Map {
	normalized := NormalizeFunctionCall(item, "custom_tool_call")
	normalized["input"] = shared.ContentToText(normalized["input"], false)
	delete(normalized, "arguments")
	return normalized
}

func InputItemsToChatMessages(items []shared.Map) []shared.Map {
	messages := []shared.Map{}
	for i := 0; i < len(items); i++ {
		item := items[i]
		itemType := shared.StringValue(item["type"])
		if itemType == "" {
			itemType = "message"
		}
		switch {
		case itemType == "message":
			if shared.StringValue(item["role"]) == "assistant" {
				calls := []shared.Map{}
				j := i + 1
				for ; j < len(items); j++ {
					candidate := items[j]
					if !IsToolCallItem(candidate) || (item["_upstream_turn_id"] != nil && !SameKimiTurn(item, candidate)) {
						break
					}
					calls = append(calls, candidate)
				}
				if len(calls) > 0 {
					messages = append(messages, AssistantToolCallMessage(calls, shared.ContentToText(item["content"], true), shared.StringValue(item["_upstream_reasoning_content"])))
					i = j - 1
					continue
				}
			}
			if msg := ResponseMessageToChatMessage(item); msg != nil {
				messages = append(messages, msg)
			}
		case IsToolCallItem(item):
			calls := []shared.Map{item}
			j := i + 1
			for ; j < len(items); j++ {
				candidate := items[j]
				if !IsToolCallItem(candidate) || (item["_upstream_turn_id"] != nil && candidate["_upstream_turn_id"] != nil && !SameKimiTurn(item, candidate)) {
					break
				}
				calls = append(calls, candidate)
			}
			content, reasoning, next := assistantContextAfterToolCalls(items, j)
			messages = append(messages, AssistantToolCallMessage(calls, content, reasoning))
			i = next - 1
		case itemType == "function_call_output" || itemType == "custom_tool_call_output":
			messages = append(messages, shared.Map{"role": "tool", "tool_call_id": item["call_id"], "content": shared.ContentToText(item["output"], false)})
		}
	}
	return messages
}

func assistantContextAfterToolCalls(items []shared.Map, start int) (string, string, int) {
	contentParts := []string{}
	reasoningParts := []string{}
	i := start
	for ; i < len(items); i++ {
		item := items[i]
		itemType := shared.StringValue(item["type"])
		if itemType == "" {
			itemType = "message"
		}
		if itemType == "function_call_output" || itemType == "custom_tool_call_output" {
			break
		}
		if itemType != "message" || shared.StringValue(item["role"]) != "assistant" {
			break
		}
		text := shared.ContentToText(item["content"], true)
		reasoning, content := splitThinkContent(text)
		if upstreamReasoning := shared.StringValue(item["_upstream_reasoning_content"]); upstreamReasoning != "" {
			reasoningParts = append(reasoningParts, upstreamReasoning)
		}
		if reasoning != "" {
			reasoningParts = append(reasoningParts, reasoning)
		}
		if content != "" {
			contentParts = append(contentParts, content)
		}
	}
	return strings.Join(contentParts, "\n\n"), strings.Join(reasoningParts, "\n\n"), i
}

func splitThinkContent(text string) (string, string) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "<think>") || !strings.HasSuffix(trimmed, "</think>") {
		return "", text
	}
	inner := strings.TrimPrefix(trimmed, "<think>")
	inner = strings.TrimSuffix(inner, "</think>")
	return strings.TrimSpace(inner), ""
}

func ResponseMessageToChatMessage(item shared.Map) shared.Map {
	role := shared.StringValue(item["role"])
	if role == "developer" {
		role = "system"
	}
	if role != "system" && role != "user" && role != "assistant" && role != "tool" {
		return nil
	}
	content := any(shared.ContentToText(item["content"], role == "assistant"))
	if role == "user" {
		content = ResponseInputContentToKimiContent(item["content"])
	}
	message := shared.Map{"role": role, "content": content}
	if role == "assistant" && shared.StringValue(item["_upstream_reasoning_content"]) != "" {
		message["reasoning_content"] = item["_upstream_reasoning_content"]
	}
	return message
}

func ResponseInputContentToKimiContent(content any) any {
	parts, hasMedia := responseInputContentParts(content)
	if !hasMedia {
		return shared.ContentToText(content, false)
	}
	out := make([]any, 0, len(parts))
	for _, part := range parts {
		out = append(out, part)
	}
	return out
}

func responseInputContentParts(content any) ([]shared.Map, bool) {
	parts := []shared.Map{}
	hasMedia := false
	appendPart := func(part shared.Map, media bool) {
		if part == nil {
			return
		}
		if media {
			hasMedia = true
		}
		parts = append(parts, part)
	}
	switch c := content.(type) {
	case string:
		appendPart(kimiTextPart(c), false)
	case []any:
		for _, raw := range c {
			part, media := responseInputPartToKimiPart(raw)
			appendPart(part, media)
		}
	case []shared.Map:
		for _, raw := range c {
			part, media := responseInputPartToKimiPart(raw)
			appendPart(part, media)
		}
	case map[string]any:
		part, media := responseInputPartToKimiPart(c)
		appendPart(part, media)
	default:
		appendPart(kimiTextPart(shared.StringValue(content)), false)
	}
	return parts, hasMedia
}

func responseInputPartToKimiPart(raw any) (shared.Map, bool) {
	switch part := raw.(type) {
	case string:
		return kimiTextPart(part), false
	case map[string]any:
		switch shared.StringValue(part["type"]) {
		case "input_text", "output_text", "text":
			return kimiTextPart(shared.StringValue(part["text"])), false
		case "input_image", "image_url":
			return kimiMediaPart("image_url", part["image_url"], part["file_id"]), true
		case "video_url":
			return kimiMediaPart("video_url", part["video_url"], part["file_id"]), true
		default:
			return nil, false
		}
	default:
		return kimiTextPart(shared.StringValue(raw)), false
	}
}

func kimiTextPart(text string) shared.Map {
	if text == "" {
		return nil
	}
	return shared.Map{"type": "text", "text": text}
}

func kimiMediaPart(kind string, value any, fileID any) shared.Map {
	url, ok := kimiMediaURL(value)
	if !ok {
		if id := shared.StringValue(fileID); strings.HasPrefix(id, "ms://") {
			url = shared.Map{"url": id}
			ok = true
		}
	}
	if !ok {
		return nil
	}
	return shared.Map{"type": kind, kind: url}
}

func kimiMediaURL(value any) (shared.Map, bool) {
	switch v := value.(type) {
	case string:
		if v != "" {
			return shared.Map{"url": v}, true
		}
	case map[string]any:
		if url := shared.StringValue(v["url"]); url != "" {
			return shared.Map{"url": url}, true
		}
	}
	return nil, false
}

func IsToolCallItem(item shared.Map) bool {
	t := shared.StringValue(item["type"])
	return t == "function_call" || t == "custom_tool_call"
}

func SameKimiTurn(left, right shared.Map) bool {
	l := shared.StringValue(left["_upstream_turn_id"])
	r := shared.StringValue(right["_upstream_turn_id"])
	return l != "" && r != "" && l == r
}

func AssistantToolCallMessage(calls []shared.Map, args ...string) shared.Map {
	content := ""
	reasoning := ""
	if len(args) > 0 {
		content = args[0]
	}
	if len(args) > 1 {
		reasoning = args[1]
	}
	message := shared.Map{"role": "assistant", "content": content, "tool_calls": []any{}}
	toolCalls := []any{}
	for _, call := range calls {
		toolCalls = append(toolCalls, FunctionCallToToolCall(call))
		if reasoning == "" {
			reasoning = shared.StringValue(call["_upstream_reasoning_content"])
		}
	}
	message["tool_calls"] = toolCalls
	if reasoning != "" {
		message["reasoning_content"] = reasoning
	}
	return message
}

func FunctionCallToToolCall(item shared.Map) shared.Map {
	callID := shared.StringValue(item["call_id"])
	if callID == "" {
		callID = shared.StringValue(item["id"])
	}
	if callID == "" {
		callID = shared.NewID("call")
	}
	name := UpstreamToolNameForResponseItem(item)
	arguments := item["arguments"]
	if shared.StringValue(item["type"]) == "custom_tool_call" {
		arguments = shared.Map{"input": shared.ContentToText(item["input"], false)}
	}
	return shared.Map{
		"id":   callID,
		"type": "function",
		"function": shared.Map{
			"name":      name,
			"arguments": shared.JSONString(arguments),
		},
	}
}

func UpstreamToolNameForResponseItem(item shared.Map) string {
	if name := shared.StringValue(item["_upstream_tool_name"]); name != "" {
		return name
	}
	return shared.SafeToolName(shared.RawToolName(item["name"], shared.StringValue(item["namespace"])))
}

func (a Adapter) BuildKimiPayload(payload shared.Map, messages []shared.Map) (shared.Map, map[string]shared.Map) {
	model := shared.StringValue(payload["model"])
	if model == "" {
		model = a.DefaultModel
	}
	chatPayload := shared.Map{"model": model, "messages": messages, "stream": shared.BoolValue(payload["stream"])}
	for source, target := range map[string]string{
		"max_output_tokens": "max_tokens",
		"temperature":       "temperature",
		"top_p":             "top_p",
		"stop":              "stop",
		"presence_penalty":  "presence_penalty",
		"frequency_penalty": "frequency_penalty",
		"logprobs":          "logprobs",
	} {
		if value, ok := payload[source]; ok && value != nil {
			chatPayload[target] = value
		}
	}
	if options, ok := payload["stream_options"].(map[string]any); ok {
		chatPayload["stream_options"] = options
	}
	tools, toolNameMap := NormalizeToolsForKimi(payload["tools"])
	if choice, ok := payload["tool_choice"].(map[string]any); ok && shared.StringValue(choice["type"]) == "allowed_tools" {
		tools = FilterToolsForAllowedChoice(tools, choice, toolNameMap)
	}
	if len(tools) > 0 {
		chatPayload["tools"] = tools
	}
	if mapped := MapToolChoice(payload["tool_choice"], toolNameMap); mapped != nil {
		chatPayload["tool_choice"] = mapped
	}
	chat.MapReasoning(payload, chatPayload)
	MapTextFormat(payload, messages, chatPayload)
	return chatPayload, toolNameMap
}

func NormalizeToolsForKimi(value any) ([]shared.Map, map[string]shared.Map) {
	rawTools, ok := value.([]any)
	if !ok {
		return nil, map[string]shared.Map{}
	}
	tools := []shared.Map{}
	used := map[string]string{}
	toolNameMap := map[string]shared.Map{}
	for _, rawTool := range rawTools {
		tool, ok := rawTool.(map[string]any)
		if !ok {
			continue
		}
		if shared.StringValue(tool["type"]) == "namespace" {
			namespace := shared.StringValue(tool["name"])
			namespaceDescription := shared.StringValue(tool["description"])
			if children, ok := tool["tools"].([]any); ok {
				for _, child := range children {
					if normalized := NormalizeToolDefinition(child, used, toolNameMap, namespace, namespaceDescription); normalized != nil {
						tools = append(tools, normalized)
					}
				}
			}
			continue
		}
		if normalized := NormalizeToolDefinition(tool, used, toolNameMap, "", ""); normalized != nil {
			tools = append(tools, normalized)
		}
	}
	return tools, toolNameMap
}

func NormalizeToolDefinition(raw any, used map[string]string, toolNameMap map[string]shared.Map, namespace, namespaceDescription string) shared.Map {
	tool, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	switch shared.StringValue(tool["type"]) {
	case "function":
		return NormalizeFunctionTool(tool, used, toolNameMap, namespace, namespaceDescription)
	case "custom":
		return NormalizeCustomTool(tool, used, toolNameMap, namespace, namespaceDescription)
	default:
		return nil
	}
}

func NormalizeFunctionTool(tool map[string]any, used map[string]string, toolNameMap map[string]shared.Map, namespace, namespaceDescription string) shared.Map {
	var function shared.Map
	var originalName string
	if fn, ok := tool["function"].(map[string]any); ok {
		function = shared.CloneMap(fn)
		originalName = shared.StringValue(function["name"])
		if originalName == "" {
			originalName = shared.StringValue(tool["name"])
		}
		if _, ok := function["description"]; !ok && tool["description"] != nil {
			function["description"] = tool["description"]
		}
		if _, ok := function["parameters"]; !ok && tool["parameters"] != nil {
			function["parameters"] = tool["parameters"]
		}
		if _, ok := function["strict"]; !ok {
			if strict, hasStrict := tool["strict"]; hasStrict {
				function["strict"] = strict
			}
		}
	} else {
		originalName = shared.StringValue(tool["name"])
		function = shared.Map{"name": originalName, "description": tool["description"], "parameters": valueOrEmptyObject(tool["parameters"])}
		if strict, ok := tool["strict"]; ok {
			function["strict"] = strict
		}
	}
	if originalName == "" {
		return nil
	}
	upstreamName := shared.UniqueToolName(originalName, namespace, used)
	function["name"] = upstreamName
	if function["parameters"] == nil {
		function["parameters"] = shared.Map{}
	}
	if namespace != "" || namespaceDescription != "" {
		function["description"] = chat.ToolDescription(function["description"], namespace, namespaceDescription, nil)
	}
	toolNameMap[upstreamName] = shared.Map{"type": "function", "name": originalName, "namespace": nilIfEmpty(namespace), "upstream_name": upstreamName}
	return shared.Map{"type": "function", "function": function}
}

func NormalizeCustomTool(tool map[string]any, used map[string]string, toolNameMap map[string]shared.Map, namespace, namespaceDescription string) shared.Map {
	originalName := shared.StringValue(tool["name"])
	if originalName == "" {
		return nil
	}
	upstreamName := shared.UniqueToolName(originalName, namespace, used)
	function := shared.Map{
		"name":        upstreamName,
		"description": chat.ToolDescription(tool["description"], namespace, namespaceDescription, tool["format"]),
		"parameters": shared.Map{
			"type": "object",
			"properties": shared.Map{
				"input": shared.Map{"type": "string", "description": "Raw input for this custom tool."},
			},
			"required":             []any{"input"},
			"additionalProperties": false,
		},
	}
	if strict, ok := tool["strict"]; ok {
		function["strict"] = strict
	}
	toolNameMap[upstreamName] = shared.Map{"type": "custom", "name": originalName, "namespace": nilIfEmpty(namespace), "upstream_name": upstreamName}
	return shared.Map{"type": "function", "function": function}
}

func MapToolChoice(toolChoice any, toolNameMap map[string]shared.Map) any {
	if text, ok := toolChoice.(string); ok && (text == "auto" || text == "none" || text == "required") {
		return text
	}
	choice, ok := toolChoice.(map[string]any)
	if !ok {
		return nil
	}
	typ := shared.StringValue(choice["type"])
	if (typ == "function" || typ == "custom") && shared.StringValue(choice["name"]) != "" {
		if name := mappedToolNameForChoice(choice["name"], shared.StringValue(choice["namespace"]), typ, toolNameMap); name != "" {
			return shared.Map{"type": "function", "function": shared.Map{"name": name}}
		}
	}
	if typ == "allowed_tools" {
		mode := shared.StringValue(choice["mode"])
		if mode == "auto" || mode == "required" {
			return mode
		}
	}
	return nil
}

func FilterToolsForAllowedChoice(tools []shared.Map, choice map[string]any, toolNameMap map[string]shared.Map) []shared.Map {
	allowed, ok := choice["tools"].([]any)
	if !ok {
		return tools
	}
	allowedNames := map[string]bool{}
	for _, entry := range allowed {
		for upstreamName, mapping := range toolNameMap {
			if AllowedEntryMatches(entry, mapping) {
				allowedNames[upstreamName] = true
			}
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

func AllowedEntryMatches(entry any, mapping shared.Map) bool {
	if text, ok := entry.(string); ok {
		return text == mapping["name"] || text == mapping["upstream_name"] || text == shared.RawToolName(mapping["name"], shared.StringValue(mapping["namespace"]))
	}
	e, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	if typ := shared.StringValue(e["type"]); typ != "" && typ != shared.StringValue(mapping["type"]) {
		return false
	}
	if name := shared.StringValue(e["name"]); name != "" && name != shared.StringValue(mapping["name"]) && name != shared.StringValue(mapping["upstream_name"]) {
		return false
	}
	if namespace := shared.StringValue(e["namespace"]); namespace != "" && namespace != shared.StringValue(mapping["namespace"]) {
		return false
	}
	return shared.StringValue(e["name"]) != "" || shared.StringValue(e["namespace"]) != ""
}

func MapTextFormat(payload shared.Map, messages []shared.Map, chatPayload shared.Map) {
	textConfig, ok := payload["text"].(map[string]any)
	if !ok {
		return
	}
	formatConfig, ok := textConfig["format"].(map[string]any)
	if !ok {
		return
	}
	switch shared.StringValue(formatConfig["type"]) {
	case "json_object":
		chatPayload["response_format"] = shared.Map{"type": "json_object"}
	case "json_schema":
		chatPayload["response_format"] = chat.KimiJSONSchemaResponseFormat(formatConfig, "response")
	}
}

func OutputMessageItem(text string, args ...string) shared.Map {
	id := ""
	status := "completed"
	if len(args) > 0 {
		id = args[0]
	}
	if len(args) > 1 && args[1] != "" {
		status = args[1]
	}
	if id == "" {
		id = shared.NewID("msg")
	}
	return shared.Map{"id": id, "type": "message", "status": status, "role": "assistant", "content": shared.AsMessageContent(text, true)}
}

func (a Adapter) OutputItemsFromChatCompletion(completion shared.Map, toolNameMap map[string]shared.Map) ([]shared.Map, string, string, []any) {
	choices, ok := completion["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil, "", "", nil
	}
	choice, _ := choices[0].(map[string]any)
	message, _ := choice["message"].(map[string]any)
	content := shared.ContentToText(message["content"], false)
	reasoning := shared.StringValue(message["reasoning_content"])
	toolCalls, _ := message["tool_calls"].([]any)
	turnID := ""
	if len(toolCalls) > 0 {
		turnID = shared.NewID("turn")
	}
	output := []shared.Map{}
	if reasoning != "" {
		output = append(output, ReasoningSummaryItem(reasoning))
	}
	if content != "" {
		item := OutputMessageItem(content)
		if turnID != "" {
			item["_upstream_turn_id"] = turnID
		}
		if reasoning != "" {
			item["_upstream_reasoning_content"] = reasoning
		}
		output = append(output, item)
	}
	for index, raw := range toolCalls {
		toolCall, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		callReasoning := ""
		if index == 0 {
			callReasoning = reasoning
		}
		output = append(output, UpstreamToolCallToResponseItem(toolCall, toolNameMap, callReasoning, turnID))
	}
	return output, content, shared.StringValue(choice["finish_reason"]), toolCalls
}

func ReasoningSummaryItem(text string, args ...string) shared.Map {
	id := ""
	status := "completed"
	if len(args) > 0 {
		id = args[0]
	}
	if len(args) > 1 && args[1] != "" {
		status = args[1]
	}
	if id == "" {
		id = shared.NewID("rs")
	}
	return shared.Map{"id": id, "type": "reasoning", "summary": []any{shared.Map{"type": "summary_text", "text": text}}, "status": status}
}

func UpstreamToolCallToResponseItem(toolCall map[string]any, toolNameMap map[string]shared.Map, reasoning, turnID string) shared.Map {
	function, _ := toolCall["function"].(map[string]any)
	arguments := shared.JSONString(function["arguments"])
	if arguments == "null" {
		arguments = ""
	}
	upstreamName := shared.StringValue(function["name"])
	if upstreamName == "" {
		upstreamName = "function"
	}
	mapping := toolNameMap[upstreamName]
	itemType := shared.StringValue(mapping["type"])
	if itemType == "" {
		itemType = "function"
	}
	name := shared.StringValue(mapping["name"])
	if name == "" {
		name = upstreamName
	}
	item := shared.Map{"id": shared.NewID("fc"), "call_id": valueOrDefault(toolCall["id"], shared.NewID("call")), "name": name, "status": "completed", "_upstream_tool_name": upstreamName}
	if itemType == "custom" {
		input := arguments
		var parsed map[string]any
		if err := jsonUnmarshal(arguments, &parsed); err == nil {
			if _, ok := parsed["input"]; ok {
				input = shared.ContentToText(parsed["input"], false)
			}
		}
		item["type"] = "custom_tool_call"
		item["input"] = input
	} else {
		item["type"] = "function_call"
		item["arguments"] = arguments
	}
	if ns := shared.StringValue(mapping["namespace"]); ns != "" {
		item["namespace"] = ns
	}
	if reasoning != "" {
		item["_upstream_reasoning_content"] = reasoning
	}
	if turnID != "" {
		item["_upstream_turn_id"] = turnID
	}
	return item
}

func ResponseUsageFromKimi(value any) any {
	usage, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	input := shared.IntValue(usage["prompt_tokens"], 0)
	output := shared.IntValue(usage["completion_tokens"], 0)
	total := shared.IntValue(usage["total_tokens"], input+output)
	cached := shared.IntValue(usage["prompt_cache_hit_tokens"], 0)
	reasoning := 0
	if details, ok := usage["completion_tokens_details"].(map[string]any); ok {
		reasoning = shared.IntValue(details["reasoning_tokens"], 0)
	}
	if v, ok := usage["reasoning_tokens"]; ok {
		reasoning = shared.IntValue(v, reasoning)
	}
	return shared.Map{
		"input_tokens":          input,
		"input_tokens_details":  shared.Map{"cached_tokens": cached},
		"output_tokens":         output,
		"output_tokens_details": shared.Map{"reasoning_tokens": reasoning},
		"total_tokens":          total,
	}
}

func StatusFromFinishReason(finish string) (string, any) {
	switch finish {
	case "length":
		return "incomplete", shared.Map{"reason": "max_output_tokens"}
	case "content_filter":
		return "incomplete", shared.Map{"reason": "content_filter"}
	default:
		return "completed", nil
	}
}

func (a Adapter) BaseResponse(payload shared.Map, responseID string, createdAt int64, status string, output []shared.Map, outputText string, usage any, incompleteDetails any) shared.Map {
	conversationID, _ := ConversationIDFromParam(payload["conversation"])
	model := shared.StringValue(payload["model"])
	if model == "" {
		model = a.DefaultModel
	}
	var conversation any
	if conversationID != "" {
		conversation = shared.Map{"id": conversationID}
	}
	completedAt := any(nil)
	if status == "completed" || status == "incomplete" || status == "cancelled" {
		completedAt = shared.NowSeconds()
	}
	reasoning := payload["reasoning"]
	if reasoning == nil {
		reasoning = shared.Map{"effort": nil, "summary": nil}
	}
	text := payload["text"]
	if text == nil {
		text = shared.Map{"format": shared.Map{"type": "text"}}
	}
	return shared.Map{
		"id":                     responseID,
		"object":                 "response",
		"created_at":             createdAt,
		"status":                 status,
		"error":                  nil,
		"incomplete_details":     incompleteDetails,
		"instructions":           payload["instructions"],
		"metadata":               metadataOrEmpty(payload["metadata"]),
		"model":                  model,
		"output":                 shared.PublicItems(output),
		"output_text":            outputText,
		"parallel_tool_calls":    valueOrDefault(payload["parallel_tool_calls"], true),
		"previous_response_id":   payload["previous_response_id"],
		"reasoning":              reasoning,
		"store":                  valueOrDefault(payload["store"], true),
		"temperature":            valueOrDefault(payload["temperature"], 1),
		"text":                   text,
		"tool_choice":            valueOrDefault(payload["tool_choice"], "auto"),
		"tools":                  valueOrDefault(payload["tools"], []any{}),
		"top_p":                  valueOrDefault(payload["top_p"], 1),
		"truncation":             valueOrDefault(payload["truncation"], "disabled"),
		"usage":                  usage,
		"user":                   payload["user"],
		"background":             valueOrDefault(payload["background"], false),
		"completed_at":           completedAt,
		"conversation":           conversation,
		"max_output_tokens":      payload["max_output_tokens"],
		"max_tool_calls":         payload["max_tool_calls"],
		"prompt":                 payload["prompt"],
		"prompt_cache_key":       payload["prompt_cache_key"],
		"prompt_cache_retention": payload["prompt_cache_retention"],
		"safety_identifier":      valueOrDefault(payload["safety_identifier"], payload["user"]),
		"service_tier":           valueOrDefault(payload["service_tier"], "auto"),
		"top_logprobs":           payload["top_logprobs"],
	}
}

func MergeStreamToolCall(existing shared.Map, delta map[string]any) {
	if delta["id"] != nil {
		existing["id"] = delta["id"]
	}
	if delta["type"] != nil {
		existing["type"] = delta["type"]
	}
	functionDelta, ok := delta["function"].(map[string]any)
	if !ok {
		return
	}
	function, ok := existing["function"].(map[string]any)
	if !ok {
		function = shared.Map{}
		existing["function"] = function
	}
	if name := shared.StringValue(functionDelta["name"]); name != "" {
		function["name"] = name
	}
	if arguments := shared.StringValue(functionDelta["arguments"]); arguments != "" {
		function["arguments"] = shared.StringValue(function["arguments"]) + arguments
	}
}

func mappedToolNameForChoice(name any, namespace, toolType string, toolNameMap map[string]shared.Map) string {
	want := shared.StringValue(name)
	if want == "" {
		return ""
	}
	for upstreamName, mapping := range toolNameMap {
		if shared.StringValue(mapping["name"]) != want && shared.StringValue(mapping["upstream_name"]) != want {
			continue
		}
		if namespace != "" && shared.StringValue(mapping["namespace"]) != namespace {
			continue
		}
		if toolType != "" && shared.StringValue(mapping["type"]) != toolType {
			continue
		}
		return upstreamName
	}
	return shared.SafeToolName(shared.RawToolName(want, namespace))
}

func valueOrEmptyObject(value any) any {
	if value == nil {
		return shared.Map{}
	}
	return value
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

func nilIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func jsonUnmarshal(text string, out any) error {
	return json.Unmarshal([]byte(text), out)
}
