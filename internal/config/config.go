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

package config

import (
	"flag"
	"fmt"
	"strings"
	"time"
)

const (
	DefaultKimiBaseURL = "https://api.moonshot.cn/v1"
	DefaultModel       = "kimi-k2.7-code"
)

type Config struct {
	Listen                  string
	APITokens               []string
	KimiAPIKey              string
	KimiBaseURL             string
	DefaultModel            string
	ModelIDs                []string
	KimiHTTPTimeout         time.Duration
	KimiMaxIdleConns        int
	KimiMaxIdleConnsPerHost int
	KimiMaxConnsPerHost     int
	ReadHeaderTimeout       time.Duration
	IdleTimeout             time.Duration
	DebugLogBody            bool
	VerifySSL               bool
}

func Parse(args []string) (Config, error) {
	fs := flag.NewFlagSet("kimi-compatible", flag.ContinueOnError)

	var apiTokenCSV string
	var modelCSV string
	var timeoutSeconds float64
	var readHeaderTimeoutSeconds float64
	var idleTimeoutSeconds float64
	cfg := Config{
		KimiMaxIdleConns:        200,
		KimiMaxIdleConnsPerHost: 100,
		VerifySSL:               true,
	}

	fs.StringVar(&cfg.Listen, "listen", ":8080", "HTTP listen address")
	fs.StringVar(&apiTokenCSV, "api-token", "", "comma-separated local bearer token list")
	fs.StringVar(&cfg.KimiAPIKey, "kimi-api-key", "", "Kimi upstream API key")
	fs.StringVar(&cfg.KimiBaseURL, "kimi-base-url", DefaultKimiBaseURL, "Kimi upstream base URL")
	fs.StringVar(&cfg.DefaultModel, "kimi-model", DefaultModel, "default Kimi model")
	fs.StringVar(&modelCSV, "kimi-models", "", "comma-separated model IDs exposed by /v1/models")
	fs.Float64Var(&timeoutSeconds, "kimi-http-timeout", 120, "Kimi HTTP timeout in seconds")
	fs.IntVar(&cfg.KimiMaxIdleConns, "kimi-max-idle-conns", cfg.KimiMaxIdleConns, "maximum idle upstream HTTP connections")
	fs.IntVar(&cfg.KimiMaxIdleConnsPerHost, "kimi-max-idle-conns-per-host", cfg.KimiMaxIdleConnsPerHost, "maximum idle upstream HTTP connections per host")
	fs.IntVar(&cfg.KimiMaxConnsPerHost, "kimi-max-conns-per-host", 0, "maximum upstream HTTP connections per host; 0 means unlimited")
	fs.Float64Var(&readHeaderTimeoutSeconds, "read-header-timeout", 10, "local HTTP read header timeout in seconds")
	fs.Float64Var(&idleTimeoutSeconds, "idle-timeout", 120, "local HTTP idle timeout in seconds")
	fs.BoolVar(&cfg.DebugLogBody, "debug-log-body", false, "log redacted request/response bodies")
	fs.BoolVar(&cfg.VerifySSL, "verify-ssl", true, "verify Kimi upstream TLS certificates")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	cfg.APITokens = splitCSV(apiTokenCSV)
	cfg.ModelIDs = splitCSV(modelCSV)
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = DefaultModel
	}
	if len(cfg.ModelIDs) == 0 {
		cfg.ModelIDs = []string{cfg.DefaultModel}
	} else if !contains(cfg.ModelIDs, cfg.DefaultModel) {
		cfg.ModelIDs = append([]string{cfg.DefaultModel}, cfg.ModelIDs...)
	}
	if cfg.KimiBaseURL == "" {
		cfg.KimiBaseURL = DefaultKimiBaseURL
	}
	if timeoutSeconds <= 0 {
		return Config{}, fmt.Errorf("--kimi-http-timeout must be positive")
	}
	if cfg.KimiMaxIdleConns < 0 {
		return Config{}, fmt.Errorf("--kimi-max-idle-conns must be non-negative")
	}
	if cfg.KimiMaxIdleConnsPerHost < 0 {
		return Config{}, fmt.Errorf("--kimi-max-idle-conns-per-host must be non-negative")
	}
	if cfg.KimiMaxConnsPerHost < 0 {
		return Config{}, fmt.Errorf("--kimi-max-conns-per-host must be non-negative")
	}
	if readHeaderTimeoutSeconds <= 0 {
		return Config{}, fmt.Errorf("--read-header-timeout must be positive")
	}
	if idleTimeoutSeconds <= 0 {
		return Config{}, fmt.Errorf("--idle-timeout must be positive")
	}
	cfg.KimiHTTPTimeout = time.Duration(timeoutSeconds * float64(time.Second))
	cfg.ReadHeaderTimeout = time.Duration(readHeaderTimeoutSeconds * float64(time.Second))
	cfg.IdleTimeout = time.Duration(idleTimeoutSeconds * float64(time.Second))
	return cfg, nil
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
