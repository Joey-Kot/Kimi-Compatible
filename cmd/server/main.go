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
	"log"
	"net/http"
	"os"

	"kimi-compatible/internal/config"
	"kimi-compatible/internal/httpapi"
	"kimi-compatible/internal/state"
	"kimi-compatible/internal/upstream/kimi"
)

func main() {
	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	upstream := kimi.NewWithTransportConfig(cfg.KimiBaseURL, cfg.KimiAPIKey, cfg.KimiHTTPTimeout, cfg.VerifySSL, kimi.TransportConfig{
		MaxIdleConns:        cfg.KimiMaxIdleConns,
		MaxIdleConnsPerHost: cfg.KimiMaxIdleConnsPerHost,
		MaxConnsPerHost:     cfg.KimiMaxConnsPerHost,
	})
	upstream.DebugLogBody = cfg.DebugLogBody
	handler := httpapi.New(cfg, upstream, state.New())
	server := newHTTPServer(cfg, handler)
	log.Printf("listening on %s", cfg.Listen)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func newHTTPServer(cfg config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}
}
