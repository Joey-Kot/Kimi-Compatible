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
	"strings"

	gemini "kimi-compatible/internal/adapters/gemini/generate"
)

func (s *Server) handleGeminiModels(w http.ResponseWriter, r *http.Request, path string) bool {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return true
	}
	model, action, ok := parseGeminiPath(path)
	if !ok {
		return false
	}
	payload, readOK := s.readJSON(w, r, false)
	if !readOK {
		return true
	}
	if action == "countTokens" {
		prepared, err := s.gemini.BuildKimiPayload(model, payload)
		if err != nil {
			openAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "")
			return true
		}
		result, err := s.countKimiTokens(r.Context(), prepared.ChatPayload)
		if err != nil {
			s.upstreamError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, gemini.CountTokensResponse(result))
		return true
	}
	prepared, err := s.gemini.BuildKimiPayload(model, payload)
	if err != nil {
		openAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "")
		return true
	}
	if action == "streamGenerateContent" {
		s.streamGeminiContent(w, r, model, prepared.ChatPayload)
		return true
	}
	if action != "generateContent" {
		return false
	}
	prepared.ChatPayload["stream"] = false
	completion, err := s.upstream.Chat(r.Context(), prepared.ChatPayload)
	if err != nil {
		s.upstreamError(w, err)
		return true
	}
	writeJSON(w, http.StatusOK, gemini.ResponseFromKimi(completion, model))
	return true
}
func parseGeminiPath(path string) (string, string, bool) {
	prefixes := []string{"/v1beta/models/", "/v1/models/"}
	rest := ""
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			rest = strings.TrimPrefix(path, prefix)
			break
		}
	}
	if rest == "" {
		return "", "", false
	}
	parts := strings.Split(rest, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
