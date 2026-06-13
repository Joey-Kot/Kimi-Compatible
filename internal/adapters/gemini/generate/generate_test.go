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

import "testing"

func TestBuildKimiPayloadMapsGeminiGenerateContent(t *testing.T) {
	adapter := Adapter{DefaultModel: "kimi-k2.7-code"}
	payload := map[string]any{
		"systemInstruction": map[string]any{"parts": []any{map[string]any{"text": "Be brief."}}},
		"contents": []any{
			map[string]any{"role": "user", "parts": []any{map[string]any{"text": "Weather?"}}},
			map[string]any{"role": "model", "parts": []any{map[string]any{"functionCall": map[string]any{"id": "call_1", "name": "get_weather", "args": map[string]any{"city": "Hangzhou"}}}}},
			map[string]any{"role": "user", "parts": []any{map[string]any{"functionResponse": map[string]any{"id": "call_1", "name": "get_weather", "response": map[string]any{"temp": "24C"}}}}},
		},
		"generationConfig": map[string]any{"maxOutputTokens": 32, "responseMimeType": "application/json", "responseSchema": map[string]any{"type": "object"}},
		"tools":            []any{map[string]any{"functionDeclarations": []any{map[string]any{"name": "get_weather", "parameters": map[string]any{"type": "object"}}}}},
		"toolConfig":       map[string]any{"functionCallingConfig": map[string]any{"mode": "ANY", "allowedFunctionNames": []any{"get_weather"}}},
	}
	prepared, err := adapter.BuildKimiPayload("gemini-3.5-flash", payload)
	if err != nil {
		t.Fatal(err)
	}
	messages := prepared.ChatPayload["messages"].([]map[string]any)
	if messages[0]["role"] != "system" {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[1]["role"] != "user" || messages[1]["content"] != "Weather?" {
		t.Fatalf("user message = %#v", messages[1])
	}
	if messages[2]["role"] != "assistant" {
		t.Fatalf("function call message = %#v", messages[2])
	}
	if messages[3]["role"] != "tool" {
		t.Fatalf("function response = %#v", messages[3])
	}
	if prepared.ChatPayload["response_format"].(map[string]any)["type"] != "json_schema" {
		t.Fatalf("response_format = %#v", prepared.ChatPayload["response_format"])
	}
	if prepared.ChatPayload["tool_choice"].(map[string]any)["function"].(map[string]any)["name"] != "get_weather" {
		t.Fatalf("tool_choice = %#v", prepared.ChatPayload["tool_choice"])
	}
}

func TestBuildKimiPayloadPreservesGeminiInlineImage(t *testing.T) {
	adapter := Adapter{DefaultModel: "kimi-k2.7-code"}
	prepared, err := adapter.BuildKimiPayload("gemini-3.5-flash", map[string]any{
		"contents": []any{map[string]any{
			"role": "user",
			"parts": []any{
				map[string]any{"text": "Describe this image."},
				map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": "abc123"}},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	messages := prepared.ChatPayload["messages"].([]map[string]any)
	content := messages[0]["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("content = %#v", content)
	}
	textPart := content[0].(map[string]any)
	if textPart["type"] != "text" || textPart["text"] != "Describe this image." {
		t.Fatalf("text part = %#v", textPart)
	}
	imagePart := content[1].(map[string]any)
	if imagePart["type"] != "image_url" {
		t.Fatalf("image part = %#v", imagePart)
	}
	imageURL := imagePart["image_url"].(map[string]any)
	if imageURL["url"] != "data:image/png;base64,abc123" {
		t.Fatalf("image url = %#v", imageURL)
	}
}

func TestResponseFromKimiMapsGeminiParts(t *testing.T) {
	completion := map[string]any{
		"id": "resp_1",
		"choices": []any{map[string]any{
			"finish_reason": "tool_calls",
			"message": map[string]any{
				"content":           "Checking.",
				"reasoning_content": "Need tool.",
				"tool_calls":        []any{map[string]any{"id": "call_1", "function": map[string]any{"name": "get_weather", "arguments": "{\"city\":\"Hangzhou\"}"}}},
			},
		}},
		"usage": map[string]any{"prompt_tokens": 2, "completion_tokens": 3, "total_tokens": 5},
	}
	response := ResponseFromKimi(completion, "gemini-3.5-flash")
	candidates := response["candidates"].([]any)
	parts := candidates[0].(map[string]any)["content"].(map[string]any)["parts"].([]any)
	if parts[0].(map[string]any)["thought"] != true {
		t.Fatalf("thought part = %#v", parts[0])
	}
	if _, ok := parts[2].(map[string]any)["functionCall"]; !ok {
		t.Fatalf("functionCall part = %#v", parts[2])
	}
}
