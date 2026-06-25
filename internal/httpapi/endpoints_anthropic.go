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
	"net/http"

	anthropic "kimi-compatible/internal/adapters/anthropic/messages"
	"kimi-compatible/internal/adapters/openai/shared"
)

func (s *Server) handleAnthropicMessages(w http.ResponseWriter, r *http.Request, path string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	payload, ok := s.readJSON(w, r, false)
	if !ok {
		return
	}
	if path == "/v1/messages/count_tokens" {
		prepared, err := s.anthropic.BuildKimiPayload(payload)
		if err != nil {
			openAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "")
			return
		}
		result, err := s.countKimiTokens(r.Context(), prepared.ChatPayload)
		if err != nil {
			s.upstreamError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, anthropic.CountTokensResponse(result))
		return
	}
	prepared, err := s.anthropic.BuildKimiPayload(payload)
	if err != nil {
		openAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "")
		return
	}
	if shared.BoolValue(payload["stream"]) {
		s.streamAnthropicMessage(w, r, payload, prepared.ChatPayload)
		return
	}
	prepared.ChatPayload["stream"] = false
	completion, err := s.upstream.Chat(r.Context(), prepared.ChatPayload)
	if err != nil {
		s.upstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, anthropic.ResponseFromKimi(completion, payload, s.cfg.DefaultModel))
}
