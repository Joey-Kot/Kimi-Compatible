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

import "testing"

func TestBuildKimiPayloadMapsChatCompletion(t *testing.T) {
	adapter := Adapter{DefaultModel: "kimi-k2.7-code"}
	payload := map[string]any{
		"model": "kimi-k2.7-code",
		"messages": []any{
			map[string]any{"role": "developer", "content": "Be brief."},
			map[string]any{"role": "user", "content": "Hi"},
		},
		"max_completion_tokens": 32,
		"temperature":           0.2,
		"functions":             []any{map[string]any{"name": "legacy_fn", "parameters": map[string]any{"type": "object", "properties": map[string]any{}}}},
		"function_call":         map[string]any{"name": "legacy_fn"},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "answer",
				"schema": map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}},
			},
		},
		"reasoning_effort": "xhigh",
	}

	chatPayload, _, err := adapter.BuildKimiPayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	messages := chatPayload["messages"].([]map[string]any)
	if got := messages[0]["role"]; got != "system" {
		t.Fatalf("developer role = %v", got)
	}
	if got := chatPayload["max_tokens"]; got != 32 {
		t.Fatalf("max_tokens = %v", got)
	}
	tools := chatPayload["tools"].([]map[string]any)
	fn := tools[0]["function"].(map[string]any)
	if got := fn["name"]; got != "legacy_fn" {
		t.Fatalf("tool name = %v", got)
	}
	choice := chatPayload["tool_choice"].(map[string]any)["function"].(map[string]any)
	if got := choice["name"]; got != "legacy_fn" {
		t.Fatalf("tool choice = %v", got)
	}
	format := chatPayload["response_format"].(map[string]any)
	if got := format["type"]; got != "json_schema" {
		t.Fatalf("response_format = %v", got)
	}
	if got := format["json_schema"].(map[string]any)["name"]; got != "answer" {
		t.Fatalf("json_schema name = %v", got)
	}
	if got := chatPayload["reasoning_effort"]; got != "max" {
		t.Fatalf("reasoning_effort = %v", got)
	}
}
