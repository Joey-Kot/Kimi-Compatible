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

package main

import (
	"net/http"
	"testing"
	"time"

	"kimi-compatible/internal/config"
)

func TestEntrypointPackageBuilds(t *testing.T) {
	// Server startup is exercised by building cmd/server; runtime behavior is
	// covered through internal/httpapi and internal/config tests.
}

func TestNewHTTPServerUsesConfiguredTimeouts(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	cfg := config.Config{
		Listen:            "127.0.0.1:0",
		ReadHeaderTimeout: 3 * time.Second,
		IdleTimeout:       45 * time.Second,
	}

	server := newHTTPServer(cfg, handler)
	if server.Addr != cfg.Listen {
		t.Fatalf("Addr = %q", server.Addr)
	}
	if server.Handler == nil {
		t.Fatal("handler was not configured")
	}
	if server.ReadHeaderTimeout != cfg.ReadHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %s", server.ReadHeaderTimeout)
	}
	if server.IdleTimeout != cfg.IdleTimeout {
		t.Fatalf("IdleTimeout = %s", server.IdleTimeout)
	}
}
