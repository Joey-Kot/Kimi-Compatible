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

	gemini "kimi-compatible/internal/adapters/gemini/generate"
	"kimi-compatible/internal/adapters/openai/shared"
	"kimi-compatible/internal/sse"
)

func (s *Server) streamGeminiContent(w http.ResponseWriter, r *http.Request, model string, chatPayload shared.Map) {
	setSSEHeaders(w)
	flusher, _ := w.(http.Flusher)
	chatPayload["stream"] = true
	err := s.upstream.StreamChat(r.Context(), chatPayload, func(chunk shared.Map) error {
		if err := sse.Data(w, gemini.StreamChunkFromKimi(chunk, model)); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if err != nil {
		_ = sse.Data(w, errorPayload(err.Error(), errorTypeForStatus(statusFromError(err))))
	}
}
