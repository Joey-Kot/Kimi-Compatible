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
	"crypto/subtle"
	"net/http"
	"strings"
)

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) bool {
	if len(s.cfg.APITokens) == 0 {
		openAIError(w, http.StatusInternalServerError, "Server is missing --api-token", "server_error", "")
		return false
	}
	if token := r.Header.Get("x-api-key"); token != "" && s.tokenMatches(token) {
		return true
	}
	if token := r.Header.Get("x-goog-api-key"); token != "" && s.tokenMatches(token) {
		return true
	}
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		openAIError(w, http.StatusUnauthorized, "Missing API key or Authorization Bearer token", "authentication_error", "")
		return false
	}
	token := strings.TrimPrefix(auth, prefix)
	if s.tokenMatches(token) {
		return true
	}
	w.Header().Set("WWW-Authenticate", "Bearer")
	openAIError(w, http.StatusUnauthorized, "Invalid authentication token", "authentication_error", "")
	return false
}

func (s *Server) tokenMatches(token string) bool {
	for _, expected := range s.cfg.APITokens {
		if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1 {
			return true
		}
	}
	return false
}

func (s *Server) setCommonHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
}
