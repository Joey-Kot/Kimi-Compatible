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

package debuglog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	MaxBodyBytes = 64 * 1024
	Redacted     = "[REDACTED]"
)

func Body(data []byte) string {
	truncated := len(data) > MaxBodyBytes
	if truncated {
		data = data[:MaxBodyBytes]
	}
	text := redactJSONBody(data)
	if truncated {
		text += fmt.Sprintf("... [truncated after %d bytes]", MaxBodyBytes)
	}
	return text
}

func MarshalBody(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("[unmarshalable body: %v]", err)
	}
	return Body(data)
}

func redactJSONBody(data []byte) string {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return string(data)
	}
	redactValue(value)
	out, err := json.Marshal(value)
	if err != nil {
		return string(data)
	}
	return string(out)
}

func redactValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if sensitiveKey(key) {
				typed[key] = Redacted
				continue
			}
			redactValue(child)
		}
	case []any:
		for _, child := range typed {
			redactValue(child)
		}
	}
}

func sensitiveKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	return key == "api_key" ||
		key == "apikey" ||
		key == "authorization" ||
		key == "password" ||
		key == "secret" ||
		key == "token" ||
		strings.HasSuffix(key, "_api_key") ||
		strings.HasSuffix(key, "_token") ||
		strings.Contains(key, "secret")
}
