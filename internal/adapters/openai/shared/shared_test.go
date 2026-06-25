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

package shared

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCloneMapReturnsIndependentCopy(t *testing.T) {
	original := Map{"nested": Map{"ok": true}}
	cloned := CloneMap(original)
	cloned["nested"].(map[string]any)["ok"] = false
	if original["nested"].(Map)["ok"] != true {
		t.Fatalf("original was mutated: %#v", original)
	}
}

func TestCloneMapPreservesJSONNumber(t *testing.T) {
	original := Map{"n": json.Number("8"), "items": []any{Map{"x": json.Number("0.2")}}}
	cloned := CloneMap(original)
	if cloned["n"] != json.Number("8") {
		t.Fatalf("number changed: %#v", cloned["n"])
	}
	nested := cloned["items"].([]any)[0].(map[string]any)
	if nested["x"] != json.Number("0.2") {
		t.Fatalf("nested number changed: %#v", nested["x"])
	}
}

func TestContentToTextHandlesStringsAndParts(t *testing.T) {
	content := []any{
		map[string]any{"type": "input_text", "text": "hello "},
		map[string]any{"type": "text", "text": "world"},
		map[string]any{"type": "image", "text": "ignored"},
	}
	if got := ContentToText(content, false); got != "hello world" {
		t.Fatalf("ContentToText = %q", got)
	}
	if got := ContentToText(content, true); got != "hello worldignored" {
		t.Fatalf("ContentToText outputText = %q", got)
	}
}

func TestPublicItemRemovesPrivateFields(t *testing.T) {
	item := PublicItem(Map{"id": "msg_1", "_secret": "hidden"})
	if item["id"] != "msg_1" {
		t.Fatalf("id missing: %#v", item)
	}
	if _, ok := item["_secret"]; ok {
		t.Fatalf("private field leaked: %#v", item)
	}
}

func TestSafeAndUniqueToolName(t *testing.T) {
	if got := SafeToolName("mcp tool.with spaces"); got != "mcp_tool_with_spaces" {
		t.Fatalf("SafeToolName = %q", got)
	}
	long := strings.Repeat("a", 100)
	if got := SafeToolName(long); len(got) > MaxToolNameLength {
		t.Fatalf("long tool name was not capped: %q", got)
	}
	used := map[string]string{}
	first := UniqueToolName("tool/a", "", used)
	second := UniqueToolName("tool a", "", used)
	if first == second {
		t.Fatalf("colliding raw names produced same kimi name: %q", first)
	}
}

func TestJSONStringPreservesStringAndMarshalsObjects(t *testing.T) {
	if got := JSONString("already-json"); got != "already-json" {
		t.Fatalf("JSONString string = %q", got)
	}
	got := JSONString(Map{"ok": true})
	var parsed map[string]bool
	if err := json.Unmarshal([]byte(got), &parsed); err != nil || !parsed["ok"] {
		t.Fatalf("JSONString object = %q err=%v", got, err)
	}
}

func TestPaginateOrdersAndHidesPrivateFields(t *testing.T) {
	items := []Map{
		{"id": "a", "_hidden": true},
		{"id": "b"},
		{"id": "c"},
	}
	page := Paginate(items, "c", 1, "desc")
	if page["first_id"] != "b" || page["last_id"] != "b" || page["has_more"] != true {
		t.Fatalf("page = %#v", page)
	}
	data := page["data"].([]Map)
	if _, ok := data[0]["_hidden"]; ok {
		t.Fatalf("private field leaked in page: %#v", data[0])
	}
}

func TestSortByCreatedThenID(t *testing.T) {
	items := []Map{{"id": "b", "created": 1}, {"id": "a", "created": 1}, {"id": "c", "created": 0}}
	SortByCreatedThenID(items)
	if got := []string{StringValue(items[0]["id"]), StringValue(items[1]["id"]), StringValue(items[2]["id"])}; got[0] != "c" || got[1] != "a" || got[2] != "b" {
		t.Fatalf("sorted ids = %#v", got)
	}
}
