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
	"kimi-compatible/internal/adapters/openai/chat"
	"kimi-compatible/internal/adapters/openai/responses"
	"kimi-compatible/internal/adapters/openai/shared"
	"net/http"
	"strings"
)

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		payload, ok := s.readJSON(w, r, false)
		if !ok {
			return
		}
		chatPayload, requestMessages, err := s.chat.BuildKimiPayload(payload)
		if err != nil {
			openAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "")
			return
		}
		if shared.BoolValue(payload["stream"]) {
			s.streamChatCompletion(w, r, payload, chatPayload)
			return
		}
		chatPayload["stream"] = false
		completion, err := s.upstream.Chat(r.Context(), chatPayload)
		if err != nil {
			s.upstreamError(w, err)
			return
		}
		openAICompletion := chat.CompletionFromKimi(completion, payload, s.cfg.DefaultModel)
		if shared.BoolValue(payload["store"]) {
			s.store.SaveChatCompletion(openAICompletion, chat.StoredMessages(requestMessages, openAICompletion, shared.StringValue(openAICompletion["id"])))
		}
		writeJSON(w, http.StatusOK, openAICompletion)
	case http.MethodGet:
		limit, order := paginationParams(r, 20, "asc")
		model := r.URL.Query().Get("model")
		items := s.store.ListChatCompletions(model, metadataFilters(r))
		writeJSON(w, http.StatusOK, shared.Paginate(items, r.URL.Query().Get("after"), limit, order))
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleStoredChatCompletion(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(rest, "/")
	id := parts[0]
	if id == "" {
		openAIError(w, http.StatusNotFound, "not found", "invalid_request_error", "")
		return
	}
	if len(parts) == 2 && parts[1] == "messages" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		messages, ok := s.store.ChatCompletionMessagesFor(id)
		if !ok {
			openAIError(w, http.StatusNotFound, "Chat completion not found: "+id, "invalid_request_error", "")
			return
		}
		limit, order := paginationParams(r, 20, "asc")
		writeJSON(w, http.StatusOK, shared.Paginate(messages, r.URL.Query().Get("after"), limit, order))
		return
	}
	if len(parts) != 1 {
		openAIError(w, http.StatusNotFound, "not found", "invalid_request_error", "")
		return
	}
	switch r.Method {
	case http.MethodGet:
		completion, ok := s.store.ChatCompletion(id)
		if !ok {
			openAIError(w, http.StatusNotFound, "Chat completion not found: "+id, "invalid_request_error", "")
			return
		}
		writeJSON(w, http.StatusOK, completion)
	case http.MethodPost:
		payload, ok := s.readJSON(w, r, false)
		if !ok {
			return
		}
		completion, ok := s.store.UpdateChatCompletion(id, payload["metadata"])
		if !ok {
			openAIError(w, http.StatusNotFound, "Chat completion not found: "+id, "invalid_request_error", "")
			return
		}
		writeJSON(w, http.StatusOK, completion)
	case http.MethodDelete:
		if !s.store.DeleteChatCompletion(id) {
			openAIError(w, http.StatusNotFound, "Chat completion not found: "+id, "invalid_request_error", "")
			return
		}
		writeJSON(w, http.StatusOK, shared.Map{"id": id, "object": "chat.completion.deleted", "deleted": true})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	payload, ok := s.readJSON(w, r, false)
	if !ok {
		return
	}
	prepared, err := s.responses.Prepare(payload)
	if err != nil {
		openAIError(w, statusForLookupError(err), err.Error(), "invalid_request_error", "")
		return
	}
	chatPayload, toolNameMap := s.responses.BuildKimiPayload(payload, prepared.Messages)
	if shared.BoolValue(payload["stream"]) {
		s.streamResponse(w, r, payload, prepared, chatPayload, toolNameMap)
		return
	}
	chatPayload["stream"] = false
	completion, err := s.upstream.Chat(r.Context(), chatPayload)
	if err != nil {
		s.upstreamError(w, err)
		return
	}
	outputItems, outputText, finishReason, _ := s.responses.OutputItemsFromChatCompletion(completion, toolNameMap)
	status, incompleteDetails := responses.StatusFromFinishReason(finishReason)
	responseID := shared.NewID("resp")
	response := s.responses.BaseResponse(payload, responseID, shared.NowSeconds(), status, outputItems, outputText, responses.ResponseUsageFromKimi(completion["usage"]), incompleteDetails)
	s.store.SaveResponse(response, prepared.AllItems, outputItems, payload["store"] != false, prepared.ConversationID, prepared.InputItems)
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleStoredResponse(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(rest, "/")
	id := parts[0]
	if id == "" {
		openAIError(w, http.StatusNotFound, "not found", "invalid_request_error", "")
		return
	}
	if len(parts) == 2 && parts[1] == "input_items" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		items, ok := s.store.ResponseInput(id)
		if !ok {
			openAIError(w, http.StatusNotFound, "Response not found: "+id, "invalid_request_error", "")
			return
		}
		limit, order := paginationParams(r, 20, "desc")
		writeJSON(w, http.StatusOK, shared.Paginate(items, r.URL.Query().Get("after"), limit, order))
		return
	}
	if len(parts) == 2 && parts[1] == "cancel" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		response, ok := s.store.UpdateResponse(id, func(item shared.Map) shared.Map {
			status := shared.StringValue(item["status"])
			if status == "queued" || status == "in_progress" {
				item["status"] = "cancelled"
				item["completed_at"] = shared.NowSeconds()
			}
			return item
		})
		if !ok {
			openAIError(w, http.StatusNotFound, "Response not found: "+id, "invalid_request_error", "")
			return
		}
		if shared.StringValue(response["status"]) != "cancelled" {
			openAIError(w, http.StatusBadRequest, "Only in-progress background responses can be cancelled", "invalid_request_error", "")
			return
		}
		writeJSON(w, http.StatusOK, response)
		return
	}
	if len(parts) != 1 {
		openAIError(w, http.StatusNotFound, "not found", "invalid_request_error", "")
		return
	}
	switch r.Method {
	case http.MethodGet:
		response, ok := s.store.Response(id)
		if !ok {
			openAIError(w, http.StatusNotFound, "Response not found: "+id, "invalid_request_error", "")
			return
		}
		writeJSON(w, http.StatusOK, response)
	case http.MethodDelete:
		if !s.store.DeleteResponse(id) {
			openAIError(w, http.StatusNotFound, "Response not found: "+id, "invalid_request_error", "")
			return
		}
		writeJSON(w, http.StatusOK, shared.Map{"id": id, "object": "response", "deleted": true})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleInputTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	payload, ok := s.readJSON(w, r, false)
	if !ok {
		return
	}
	prepared, err := s.responses.Prepare(payload)
	if err != nil {
		openAIError(w, statusForLookupError(err), err.Error(), "invalid_request_error", "")
		return
	}
	result, err := s.countKimiTokens(r.Context(), shared.Map{
		"model":    valueOrDefault(payload["model"], s.cfg.DefaultModel),
		"messages": prepared.Messages,
	})
	if err != nil {
		s.upstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, shared.Map{"object": "response.input_tokens", "input_tokens": result})
}

func (s *Server) handleCompact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	payload, ok := s.readJSON(w, r, false)
	if !ok {
		return
	}
	compactPayload := shared.CloneMap(payload)
	if shared.StringValue(compactPayload["instructions"]) == "" {
		compactPayload["instructions"] = "Compact the provided conversation into a concise context summary for future turns."
	}
	if compactPayload["text"] == nil {
		compactPayload["text"] = shared.Map{"format": shared.Map{"type": "text"}}
	}
	prepared, err := s.responses.Prepare(compactPayload)
	if err != nil {
		openAIError(w, statusForLookupError(err), err.Error(), "invalid_request_error", "")
		return
	}
	chatPayload, toolNameMap := s.responses.BuildKimiPayload(compactPayload, prepared.Messages)
	chatPayload["stream"] = false
	completion, err := s.upstream.Chat(r.Context(), chatPayload)
	if err != nil {
		s.upstreamError(w, err)
		return
	}
	outputItems, _, finishReason, _ := s.responses.OutputItemsFromChatCompletion(completion, toolNameMap)
	status, _ := responses.StatusFromFinishReason(finishReason)
	writeJSON(w, http.StatusOK, shared.Map{
		"id":         shared.NewID("comp"),
		"created_at": shared.NowSeconds(),
		"object":     "response.compaction",
		"status":     status,
		"output":     shared.PublicItems(outputItems),
		"usage":      responses.ResponseUsageFromKimi(completion["usage"]),
	})
}

func (s *Server) handleConversations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	payload, ok := s.readJSON(w, r, true)
	if !ok {
		return
	}
	id := shared.NewID("conv")
	conversation := shared.Map{"id": id, "object": "conversation", "created_at": shared.NowSeconds(), "metadata": metadataOrEmpty(payload["metadata"])}
	items, err := s.responses.NormalizeInputItems(payload["items"])
	if err != nil {
		openAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "")
		return
	}
	s.store.SaveConversation(conversation, items)
	writeJSON(w, http.StatusOK, conversation)
}

func (s *Server) handleStoredConversation(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" || strings.Contains(id, "/") {
		openAIError(w, http.StatusNotFound, "not found", "invalid_request_error", "")
		return
	}
	switch r.Method {
	case http.MethodGet:
		conversation, ok := s.store.Conversation(id)
		if !ok {
			openAIError(w, http.StatusNotFound, "Conversation not found: "+id, "invalid_request_error", "")
			return
		}
		writeJSON(w, http.StatusOK, conversation)
	case http.MethodPost, http.MethodPatch:
		payload, ok := s.readJSON(w, r, false)
		if !ok {
			return
		}
		conversation, ok := s.store.UpdateConversation(id, payload["metadata"])
		if !ok {
			openAIError(w, http.StatusNotFound, "Conversation not found: "+id, "invalid_request_error", "")
			return
		}
		writeJSON(w, http.StatusOK, conversation)
	case http.MethodDelete:
		if !s.store.DeleteConversation(id) {
			openAIError(w, http.StatusNotFound, "Conversation not found: "+id, "invalid_request_error", "")
			return
		}
		writeJSON(w, http.StatusOK, shared.Map{"id": id, "deleted": true, "object": "conversation.deleted"})
	default:
		methodNotAllowed(w)
	}
}
