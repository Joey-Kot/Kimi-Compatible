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
	"context"
	"kimi-compatible/internal/adapters/openai/shared"
	"net/http"
)

func (s *Server) handleKimiChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	payload, ok := s.readJSON(w, r, false)
	if !ok {
		return
	}
	if shared.StringValue(payload["model"]) == "" {
		payload["model"] = s.cfg.DefaultModel
	}
	if shared.BoolValue(payload["stream"]) {
		s.streamKimiChatCompletion(w, r, payload)
		return
	}
	payload["stream"] = false
	completion, err := s.upstream.Chat(r.Context(), payload)
	if err != nil {
		s.upstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, completion)
}

func (s *Server) handleKimiTokenEstimate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	payload, ok := s.readJSON(w, r, false)
	if !ok {
		return
	}
	if shared.StringValue(payload["model"]) == "" {
		payload["model"] = s.cfg.DefaultModel
	}
	result, err := s.upstream.EstimateTokens(r.Context(), payload)
	if err != nil {
		s.upstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) countKimiTokens(ctx context.Context, payload shared.Map) (int, error) {
	result, err := s.upstream.EstimateTokens(ctx, shared.Map{
		"model":    valueOrDefault(payload["model"], s.cfg.DefaultModel),
		"messages": payload["messages"],
	})
	if err != nil {
		return 0, err
	}
	if data, ok := result["data"].(map[string]any); ok {
		return shared.IntValue(data["total_tokens"], 0), nil
	}
	return shared.IntValue(result["total_tokens"], 0), nil
}
