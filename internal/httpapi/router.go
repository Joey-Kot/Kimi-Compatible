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

package httpapi

import (
	"log"
	"net/http"
	"runtime"
	"strings"

	"kimi-compatible/internal/adapters/openai/shared"
	"kimi-compatible/internal/debuglog"
)

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.cfg.DebugLogBody {
		debugWriter := newDebugResponseWriter(w)
		s.serveHTTP(debugWriter, r)
		log.Printf("debug body response method=%s path=%s status=%d body=%s", r.Method, r.URL.RequestURI(), debugWriter.statusCode(), debuglog.Body(debugWriter.bodyBytes()))
		return
	}
	s.serveHTTP(w, r)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	s.setCommonHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Path == "/health" {
		writeJSON(w, http.StatusOK, shared.Map{"status": "ok"})
		return
	}
	if !s.authorize(w, r) {
		return
	}

	path := strings.TrimRight(r.URL.Path, "/")
	if path == "" {
		path = "/"
	}
	switch {
	case path == "/kimi/v1/chat/completions":
		s.handleKimiChatCompletions(w, r)
	case path == "/v1/messages" || path == "/v1/messages/count_tokens":
		s.handleAnthropicMessages(w, r, path)
	case strings.HasPrefix(path, "/v1beta/models/") || strings.HasPrefix(path, "/v1/models/"):
		if s.handleGeminiModels(w, r, path) {
			return
		}
		openAIError(w, http.StatusNotFound, "not found", "invalid_request_error", "")
	case r.Method == http.MethodGet && path == "/v1/models":
		s.handleModels(w, r)
	case r.Method == http.MethodGet && path == "/healthz/memory":
		s.handleMemoryHealth(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/debug/"):
		s.handleDebug(w, r, path)
	case path == "/v1/chat/completions":
		s.handleChatCompletions(w, r)
	case strings.HasPrefix(path, "/v1/chat/completions/"):
		s.handleStoredChatCompletion(w, r, strings.TrimPrefix(path, "/v1/chat/completions/"))
	case path == "/v1/responses":
		s.handleResponses(w, r)
	case path == "/v1/responses/input_tokens":
		s.handleInputTokens(w, r)
	case path == "/v1/tokenizers/estimate-token-count":
		s.handleKimiTokenEstimate(w, r)
	case path == "/v1/responses/compact":
		s.handleCompact(w, r)
	case strings.HasPrefix(path, "/v1/responses/"):
		s.handleStoredResponse(w, r, strings.TrimPrefix(path, "/v1/responses/"))
	case path == "/v1/conversations":
		s.handleConversations(w, r)
	case strings.HasPrefix(path, "/v1/conversations/"):
		s.handleStoredConversation(w, r, strings.TrimPrefix(path, "/v1/conversations/"))
	default:
		openAIError(w, http.StatusNotFound, "not found", "invalid_request_error", "")
	}
}

func (s *Server) handleModels(w http.ResponseWriter, _ *http.Request) {
	data := []any{}
	for _, model := range s.cfg.ModelIDs {
		data = append(data, shared.Map{"id": model, "object": "model", "owned_by": "moonshot"})
	}
	writeJSON(w, http.StatusOK, shared.Map{"object": "list", "data": data})
}

func (s *Server) handleMemoryHealth(w http.ResponseWriter, _ *http.Request) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	writeJSON(w, http.StatusOK, shared.Map{
		"alloc":      mem.Alloc,
		"sys":        mem.Sys,
		"num_gc":     mem.NumGC,
		"goroutines": runtime.NumGoroutine(),
		"store":      s.store.Stats(),
	})
}
