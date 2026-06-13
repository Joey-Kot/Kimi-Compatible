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
	"strings"
	"testing"
)

func TestBodyRedactsSensitiveJSONFields(t *testing.T) {
	body := Body([]byte(`{"api_key":"sk-real","nested":{"authorization":"Bearer token","value":"ok"}}`))
	if strings.Contains(body, "sk-real") || strings.Contains(body, "Bearer token") {
		t.Fatalf("body was not redacted: %s", body)
	}
	if !strings.Contains(body, `"api_key":"[REDACTED]"`) || !strings.Contains(body, `"value":"ok"`) {
		t.Fatalf("body = %s", body)
	}
}

func TestBodyKeepsNonJSONText(t *testing.T) {
	body := Body([]byte("plain text"))
	if body != "plain text" {
		t.Fatalf("body = %q", body)
	}
}

func TestBodyTruncatesLargePayloads(t *testing.T) {
	body := Body([]byte(strings.Repeat("x", MaxBodyBytes+1)))
	if !strings.Contains(body, "truncated after") {
		t.Fatalf("body was not marked as truncated: %s", body)
	}
}
