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
	"reflect"
	"testing"
	"time"
)

func TestParseDefaults(t *testing.T) {
	cfg, err := Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":8080" {
		t.Fatalf("Listen = %q", cfg.Listen)
	}
	if cfg.KimiBaseURL != DefaultKimiBaseURL {
		t.Fatalf("KimiBaseURL = %q", cfg.KimiBaseURL)
	}
	if cfg.DefaultModel != DefaultModel {
		t.Fatalf("DefaultModel = %q", cfg.DefaultModel)
	}
	if !reflect.DeepEqual(cfg.ModelIDs, []string{DefaultModel}) {
		t.Fatalf("ModelIDs = %#v", cfg.ModelIDs)
	}
	if cfg.KimiHTTPTimeout != 120*time.Second {
		t.Fatalf("KimiHTTPTimeout = %s", cfg.KimiHTTPTimeout)
	}
	if cfg.KimiMaxIdleConns != 200 {
		t.Fatalf("KimiMaxIdleConns = %d", cfg.KimiMaxIdleConns)
	}
	if cfg.KimiMaxIdleConnsPerHost != 100 {
		t.Fatalf("KimiMaxIdleConnsPerHost = %d", cfg.KimiMaxIdleConnsPerHost)
	}
	if cfg.KimiMaxConnsPerHost != 0 {
		t.Fatalf("KimiMaxConnsPerHost = %d", cfg.KimiMaxConnsPerHost)
	}
	if cfg.StoreMaxResponses != 1000 {
		t.Fatalf("StoreMaxResponses = %d", cfg.StoreMaxResponses)
	}
	if cfg.StoreMaxChatCompletions != 1000 {
		t.Fatalf("StoreMaxChatCompletions = %d", cfg.StoreMaxChatCompletions)
	}
	if cfg.StoreMaxConversations != 1000 {
		t.Fatalf("StoreMaxConversations = %d", cfg.StoreMaxConversations)
	}
	if cfg.MaxRequestBodyBytes != 16<<20 {
		t.Fatalf("MaxRequestBodyBytes = %d", cfg.MaxRequestBodyBytes)
	}
	if cfg.ReadHeaderTimeout != 10*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s", cfg.ReadHeaderTimeout)
	}
	if cfg.IdleTimeout != 120*time.Second {
		t.Fatalf("IdleTimeout = %s", cfg.IdleTimeout)
	}
	if !cfg.VerifySSL {
		t.Fatal("VerifySSL should default to true")
	}
}

func TestParseCommandLineFlags(t *testing.T) {
	cfg, err := Parse([]string{
		"--listen", "127.0.0.1:9999",
		"--api-token", "sk-a, sk-b ,,",
		"--kimi-api-key", "sk-upstream",
		"--kimi-base-url", "https://example.test/v1",
		"--kimi-model", "kimi-k2.6",
		"--kimi-models", "kimi-k2.7-code,kimi-k2.6",
		"--kimi-http-timeout", "2.5",
		"--kimi-max-idle-conns", "300",
		"--kimi-max-idle-conns-per-host", "150",
		"--kimi-max-conns-per-host", "80",
		"--store-max-responses", "10",
		"--store-max-chat-completions", "11",
		"--store-max-conversations", "12",
		"--max-request-body-bytes", "4096",
		"--read-header-timeout", "3.5",
		"--idle-timeout", "45",
		"--debug-pprof=true",
		"--debug-log-body=true",
		"--verify-ssl=false",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.APITokens, []string{"sk-a", "sk-b"}) {
		t.Fatalf("APITokens = %#v", cfg.APITokens)
	}
	if cfg.KimiAPIKey != "sk-upstream" {
		t.Fatalf("KimiAPIKey = %q", cfg.KimiAPIKey)
	}
	if !reflect.DeepEqual(cfg.ModelIDs, []string{"kimi-k2.7-code", "kimi-k2.6"}) {
		t.Fatalf("ModelIDs = %#v", cfg.ModelIDs)
	}
	if cfg.KimiHTTPTimeout != 2500*time.Millisecond {
		t.Fatalf("KimiHTTPTimeout = %s", cfg.KimiHTTPTimeout)
	}
	if cfg.KimiMaxIdleConns != 300 {
		t.Fatalf("KimiMaxIdleConns = %d", cfg.KimiMaxIdleConns)
	}
	if cfg.KimiMaxIdleConnsPerHost != 150 {
		t.Fatalf("KimiMaxIdleConnsPerHost = %d", cfg.KimiMaxIdleConnsPerHost)
	}
	if cfg.KimiMaxConnsPerHost != 80 {
		t.Fatalf("KimiMaxConnsPerHost = %d", cfg.KimiMaxConnsPerHost)
	}
	if cfg.StoreMaxResponses != 10 {
		t.Fatalf("StoreMaxResponses = %d", cfg.StoreMaxResponses)
	}
	if cfg.StoreMaxChatCompletions != 11 {
		t.Fatalf("StoreMaxChatCompletions = %d", cfg.StoreMaxChatCompletions)
	}
	if cfg.StoreMaxConversations != 12 {
		t.Fatalf("StoreMaxConversations = %d", cfg.StoreMaxConversations)
	}
	if cfg.MaxRequestBodyBytes != 4096 {
		t.Fatalf("MaxRequestBodyBytes = %d", cfg.MaxRequestBodyBytes)
	}
	if cfg.ReadHeaderTimeout != 3500*time.Millisecond {
		t.Fatalf("ReadHeaderTimeout = %s", cfg.ReadHeaderTimeout)
	}
	if cfg.IdleTimeout != 45*time.Second {
		t.Fatalf("IdleTimeout = %s", cfg.IdleTimeout)
	}
	if !cfg.DebugPprof || !cfg.DebugLogBody {
		t.Fatalf("boolean flags were not parsed: %#v", cfg)
	}
	if cfg.VerifySSL {
		t.Fatalf("VerifySSL = %t", cfg.VerifySSL)
	}
}

func TestParsePrependsDefaultModelWhenMissingFromModelList(t *testing.T) {
	cfg, err := Parse([]string{"--kimi-model", "kimi-k2.7-code", "--kimi-models", "kimi-k2.6"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.ModelIDs, []string{"kimi-k2.7-code", "kimi-k2.6"}) {
		t.Fatalf("ModelIDs = %#v", cfg.ModelIDs)
	}
}

func TestParseRejectsNonPositiveTimeout(t *testing.T) {
	if _, err := Parse([]string{"--kimi-http-timeout", "0"}); err == nil {
		t.Fatal("expected error for zero timeout")
	}
}

func TestParseRejectsInvalidConnectionLimits(t *testing.T) {
	for _, flag := range []string{"--kimi-max-idle-conns", "--kimi-max-idle-conns-per-host", "--kimi-max-conns-per-host"} {
		if _, err := Parse([]string{flag, "-1"}); err == nil {
			t.Fatalf("expected error for %s", flag)
		}
	}
}

func TestParseRejectsInvalidMemoryLimits(t *testing.T) {
	for _, flag := range []string{"--store-max-responses", "--store-max-chat-completions", "--store-max-conversations", "--max-request-body-bytes"} {
		if _, err := Parse([]string{flag, "-1"}); err == nil {
			t.Fatalf("expected error for %s", flag)
		}
	}
}

func TestParseRejectsNonPositiveServerTimeouts(t *testing.T) {
	for _, flag := range []string{"--read-header-timeout", "--idle-timeout"} {
		if _, err := Parse([]string{flag, "0"}); err == nil {
			t.Fatalf("expected error for %s", flag)
		}
	}
}
