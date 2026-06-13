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

package sse

import (
	"bytes"
	"strings"
	"testing"
)

func TestEventWritesNamedSSEEvent(t *testing.T) {
	var buf bytes.Buffer
	if err := Event(&buf, "message_start", map[string]any{"type": "message_start"}); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.HasPrefix(got, "event: message_start\n") {
		t.Fatalf("event output = %q", got)
	}
	if !strings.Contains(got, `data: {"type":"message_start"}`) {
		t.Fatalf("event data = %q", got)
	}
	if !strings.HasSuffix(got, "\n\n") {
		t.Fatalf("event did not terminate with blank line: %q", got)
	}
}

func TestDataWritesRawStringOrJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := Data(&buf, "[DONE]"); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "data: [DONE]\n\n" {
		t.Fatalf("raw data = %q", got)
	}
	buf.Reset()
	if err := Data(&buf, map[string]any{"ok": true}); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "data: {\"ok\":true}\n\n" {
		t.Fatalf("json data = %q", got)
	}
}
