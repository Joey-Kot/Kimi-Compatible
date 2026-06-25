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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Map = map[string]any

var toolNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

const MaxToolNameLength = 64

func NewID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func NowSeconds() int64 {
	return time.Now().Unix()
}

func CloneMap(value Map) Map {
	if value == nil {
		return nil
	}
	return cloneAny(value).(map[string]any)
}

func CloneSlice(value []Map) []Map {
	if value == nil {
		return nil
	}
	out := make([]Map, len(value))
	for i, item := range value {
		out[i] = CloneMap(item)
	}
	return out
}

func cloneAny(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = cloneAny(item)
		}
		return out
	case []Map:
		out := make([]Map, len(v))
		for i, item := range v {
			out[i] = CloneMap(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneAny(item)
		}
		return out
	default:
		return v
	}
}

func AsMap(value any) (Map, bool) {
	m, ok := value.(map[string]any)
	return m, ok
}

func AsSlice(value any) ([]any, bool) {
	items, ok := value.([]any)
	return items, ok
}

func StringValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func BoolValue(value any) bool {
	v, ok := value.(bool)
	return ok && v
}

func IntValue(value any, fallback int) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		i, err := v.Int64()
		if err == nil {
			return int(i)
		}
	}
	return fallback
}

func ContentPartToText(part any, outputText bool) string {
	switch p := part.(type) {
	case string:
		return p
	case map[string]any:
		t := StringValue(p["type"])
		if t == "input_text" || t == "output_text" || t == "text" {
			return StringValue(p["text"])
		}
		if outputText {
			return StringValue(p["text"])
		}
	}
	return ""
}

func ContentToText(content any, outputText bool) string {
	switch c := content.(type) {
	case nil:
		return ""
	case string:
		return c
	case []any:
		var b strings.Builder
		for _, part := range c {
			b.WriteString(ContentPartToText(part, outputText))
		}
		return b.String()
	case []Map:
		var b strings.Builder
		for _, part := range c {
			b.WriteString(ContentPartToText(part, outputText))
		}
		return b.String()
	case map[string]any:
		return ContentPartToText(c, outputText)
	default:
		return fmt.Sprint(c)
	}
}

func AsMessageContent(text string, output bool) []Map {
	if output {
		return []Map{{"type": "output_text", "text": text, "annotations": []any{}}}
	}
	return []Map{{"type": "input_text", "text": text}}
}

func PublicItem(item Map) Map {
	out := Map{}
	for key, value := range item {
		if !strings.HasPrefix(key, "_") {
			out[key] = value
		}
	}
	return CloneMap(out)
}

func PublicItems(items []Map) []Map {
	out := make([]Map, 0, len(items))
	for _, item := range items {
		out = append(out, PublicItem(item))
	}
	return out
}

func JSONString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

func RawToolName(name any, namespace string) string {
	raw := StringValue(name)
	if raw == "" {
		raw = "tool"
	}
	if namespace != "" {
		return namespace + "__" + raw
	}
	return raw
}

func SafeToolName(raw string) string {
	safe := strings.Trim(toolNameUnsafe.ReplaceAllString(raw, "_"), "_")
	if safe == "" {
		safe = "tool"
	}
	if len(safe) <= MaxToolNameLength {
		return safe
	}
	digest := shortHash(raw, 12)
	prefixLen := MaxToolNameLength - len(digest) - 1
	return strings.TrimRight(safe[:prefixLen], "_-") + "_" + digest
}

func UniqueToolName(name any, namespace string, used map[string]string) string {
	raw := RawToolName(name, namespace)
	candidate := SafeToolName(raw)
	if old, ok := used[candidate]; !ok || old == raw {
		used[candidate] = raw
		return candidate
	}
	digest := shortHash(raw, 8)
	prefixLen := MaxToolNameLength - len(digest) - 1
	base := candidate
	if len(base) > prefixLen {
		base = base[:prefixLen]
	}
	candidate = strings.TrimRight(base, "_-") + "_" + digest
	for suffix := 2; ; suffix++ {
		if old, ok := used[candidate]; !ok || old == raw {
			used[candidate] = raw
			return candidate
		}
		s := fmt.Sprint(suffix)
		prefixLen = MaxToolNameLength - len(digest) - len(s) - 2
		base = candidate
		if len(base) > prefixLen {
			base = base[:prefixLen]
		}
		candidate = strings.TrimRight(base, "_-") + "_" + digest + "_" + s
	}
}

func shortHash(value string, n int) string {
	// FNV would be enough for uniqueness, but a JSON-stable byte digest makes
	// renamed/generated tools easier to compare across ports.
	var b [16]byte
	copy(b[:], []byte(value))
	for i := range value {
		b[i%16] ^= value[i] + byte(i)
	}
	encoded := hex.EncodeToString(b[:])
	if n > len(encoded) {
		n = len(encoded)
	}
	return encoded[:n]
}

func valueOrDefaultString(value any, fallback string) string {
	text := StringValue(value)
	if text == "" {
		return fallback
	}
	return text
}

func Paginate(items []Map, after string, limit int, order string) Map {
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	ordered := CloneSlice(items)
	if order != "asc" {
		for i, j := 0, len(ordered)-1; i < j; i, j = i+1, j-1 {
			ordered[i], ordered[j] = ordered[j], ordered[i]
		}
	}
	if after != "" {
		next := []Map{}
		for i, item := range ordered {
			if StringValue(item["id"]) == after {
				next = ordered[i+1:]
				break
			}
		}
		ordered = next
	}
	page := ordered
	if len(page) > limit {
		page = page[:limit]
	}
	var first any
	var last any
	if len(page) > 0 {
		first = page[0]["id"]
		last = page[len(page)-1]["id"]
	}
	return Map{
		"object":   "list",
		"data":     PublicItems(page),
		"first_id": first,
		"last_id":  last,
		"has_more": len(ordered) > limit,
	}
}

func SortByCreatedThenID(items []Map) {
	sort.Slice(items, func(i, j int) bool {
		ci := IntValue(items[i]["created"], 0)
		cj := IntValue(items[j]["created"], 0)
		if ci == cj {
			return StringValue(items[i]["id"]) < StringValue(items[j]["id"])
		}
		return ci < cj
	})
}
