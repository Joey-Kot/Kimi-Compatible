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
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/pprof"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	anthropic "kimi-compatible/internal/adapters/anthropic/messages"
	gemini "kimi-compatible/internal/adapters/gemini/generate"
	"kimi-compatible/internal/adapters/openai/chat"
	"kimi-compatible/internal/adapters/openai/responses"
	"kimi-compatible/internal/adapters/openai/shared"
	"kimi-compatible/internal/config"
	"kimi-compatible/internal/debuglog"
	"kimi-compatible/internal/sse"
	"kimi-compatible/internal/state"
	"kimi-compatible/internal/upstream/kimi"
)

type Upstream interface {
	Chat(ctx context.Context, payload shared.Map) (shared.Map, error)
	StreamChat(ctx context.Context, payload shared.Map, handle func(shared.Map) error) error
	EstimateTokens(ctx context.Context, payload shared.Map) (shared.Map, error)
}

type Server struct {
	cfg       config.Config
	store     *state.Store
	upstream  Upstream
	chat      chat.Adapter
	responses responses.Adapter
	anthropic anthropic.Adapter
	gemini    gemini.Adapter
}

func New(cfg config.Config, upstream Upstream, store *state.Store) *Server {
	if store == nil {
		store = state.New()
	}
	return &Server{
		cfg:       cfg,
		store:     store,
		upstream:  upstream,
		chat:      chat.Adapter{DefaultModel: cfg.DefaultModel},
		responses: responses.Adapter{DefaultModel: cfg.DefaultModel, Store: store},
		anthropic: anthropic.Adapter{DefaultModel: cfg.DefaultModel},
		gemini:    gemini.Adapter{DefaultModel: cfg.DefaultModel},
	}
}

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

func (s *Server) handleDebug(w http.ResponseWriter, r *http.Request, path string) {
	if !s.cfg.DebugPprof {
		openAIError(w, http.StatusNotFound, "not found", "invalid_request_error", "")
		return
	}
	switch path {
	case "/debug/vars":
		s.handleMemoryHealth(w, r)
	case "/debug/pprof":
		pprof.Index(w, r)
	case "/debug/pprof/cmdline":
		pprof.Cmdline(w, r)
	case "/debug/pprof/profile":
		pprof.Profile(w, r)
	case "/debug/pprof/symbol":
		pprof.Symbol(w, r)
	case "/debug/pprof/trace":
		pprof.Trace(w, r)
	default:
		name := strings.TrimPrefix(path, "/debug/pprof/")
		if name == "" || strings.Contains(name, "/") {
			openAIError(w, http.StatusNotFound, "not found", "invalid_request_error", "")
			return
		}
		pprof.Handler(name).ServeHTTP(w, r)
	}
}

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

func (s *Server) streamKimiChatCompletion(w http.ResponseWriter, r *http.Request, payload shared.Map) {
	setSSEHeaders(w)
	payload["stream"] = true
	flusher, _ := w.(http.Flusher)
	err := s.upstream.StreamChat(r.Context(), payload, func(chunk shared.Map) error {
		if err := sse.Data(w, chunk); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if err != nil {
		_ = sse.Data(w, errorPayload(err.Error(), errorTypeForStatus(statusFromError(err))))
		return
	}
	_ = sse.Data(w, "[DONE]")
	if flusher != nil {
		flusher.Flush()
	}
}

func (s *Server) streamChatCompletion(w http.ResponseWriter, r *http.Request, payload, chatPayload shared.Map) {
	setSSEHeaders(w)
	chatPayload["stream"] = true
	if options, ok := payload["stream_options"].(map[string]any); ok {
		chatPayload["stream_options"] = options
	}
	flusher, _ := w.(http.Flusher)
	err := s.upstream.StreamChat(r.Context(), chatPayload, func(chunk shared.Map) error {
		if err := sse.Data(w, chat.SSEData(chunk, payload, s.cfg.DefaultModel)); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if err != nil {
		_ = sse.Data(w, errorPayload(err.Error(), errorTypeForStatus(statusFromError(err))))
		return
	}
	_ = sse.Data(w, "[DONE]")
	if flusher != nil {
		flusher.Flush()
	}
}

func (s *Server) streamAnthropicMessage(w http.ResponseWriter, r *http.Request, payload, chatPayload shared.Map) {
	setSSEHeaders(w)
	flusher, _ := w.(http.Flusher)
	messageID := shared.NewID("msg")
	if err := writeSSEEvent(w, "message_start", anthropic.StreamStart(messageID, payload, s.cfg.DefaultModel)); err != nil {
		return
	}
	if flusher != nil {
		flusher.Flush()
	}
	textStarted := false
	thinkingStarted := false
	textIndex := 0
	toolCalls := map[int]shared.Map{}
	var usage any
	stopReason := any("end_turn")
	err := s.upstream.StreamChat(r.Context(), chatPayload, func(chunk shared.Map) error {
		if u := anthropic.UsageFromKimi(chunk["usage"]); u != nil {
			usage = u
		}
		choices, ok := chunk["choices"].([]any)
		if !ok || len(choices) == 0 {
			return nil
		}
		choice, _ := choices[0].(map[string]any)
		if fr := shared.StringValue(choice["finish_reason"]); fr != "" {
			stopReason = anthropic.StopReason(fr, nil)
		}
		delta, _ := choice["delta"].(map[string]any)
		if delta == nil {
			return nil
		}
		if reasoning := shared.StringValue(delta["reasoning_content"]); reasoning != "" {
			if !thinkingStarted {
				thinkingStarted = true
				textIndex = 1
				if err := writeSSEEvent(w, "content_block_start", shared.Map{"type": "content_block_start", "index": 0, "content_block": shared.Map{"type": "thinking", "thinking": "", "signature": ""}}); err != nil {
					return err
				}
			}
			if err := writeSSEEvent(w, "content_block_delta", anthropic.ThinkingDelta(reasoning)); err != nil {
				return err
			}
		}
		if text := shared.StringValue(delta["content"]); text != "" {
			if !textStarted {
				textStarted = true
				if err := writeSSEEvent(w, "content_block_start", shared.Map{"type": "content_block_start", "index": textIndex, "content_block": shared.Map{"type": "text", "text": ""}}); err != nil {
					return err
				}
			}
			event := anthropic.TextDelta(text)
			event["index"] = textIndex
			if err := writeSSEEvent(w, "content_block_delta", event); err != nil {
				return err
			}
		}
		if rawCalls, ok := delta["tool_calls"].([]any); ok {
			stopReason = "tool_use"
			for _, raw := range rawCalls {
				call, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				index := shared.IntValue(call["index"], 0)
				existing := toolCalls[index]
				if existing == nil {
					existing = shared.Map{"type": "function", "function": shared.Map{}}
					toolCalls[index] = existing
				}
				responses.MergeStreamToolCall(existing, call)
			}
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if err != nil {
		_ = writeSSEEvent(w, "error", errorPayload(err.Error(), errorTypeForStatus(statusFromError(err))))
		return
	}
	if thinkingStarted {
		_ = writeSSEEvent(w, "content_block_stop", shared.Map{"type": "content_block_stop", "index": 0})
	}
	if textStarted {
		_ = writeSSEEvent(w, "content_block_stop", shared.Map{"type": "content_block_stop", "index": textIndex})
	}
	nextIndex := 0
	if thinkingStarted {
		nextIndex++
	}
	if textStarted {
		nextIndex++
	}
	for i := 0; i < len(toolCalls); i++ {
		call := toolCalls[i]
		if call == nil {
			continue
		}
		block := anthropic.ContentBlocksFromMessage(map[string]any{"tool_calls": []any{call}})[0]
		_ = writeSSEEvent(w, "content_block_start", shared.Map{"type": "content_block_start", "index": nextIndex, "content_block": block})
		_ = writeSSEEvent(w, "content_block_stop", shared.Map{"type": "content_block_stop", "index": nextIndex})
		nextIndex++
	}
	_ = writeSSEEvent(w, "message_delta", anthropic.MessageDelta(stopReason, usage))
	_ = writeSSEEvent(w, "message_stop", shared.Map{"type": "message_stop"})
	if flusher != nil {
		flusher.Flush()
	}
}

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

func (s *Server) streamResponse(w http.ResponseWriter, r *http.Request, payload shared.Map, prepared responses.Prepared, chatPayload shared.Map, toolNameMap map[string]shared.Map) {
	setSSEHeaders(w)
	flusher, _ := w.(http.Flusher)
	responseID := shared.NewID("resp")
	messageID := shared.NewID("msg")
	createdAt := shared.NowSeconds()
	shell := s.responses.BaseResponse(payload, responseID, createdAt, "in_progress", nil, "", nil, nil)
	if err := writeSSEEvent(w, "response.created", shared.Map{"type": "response.created", "response": shell}); err != nil {
		return
	}
	if err := writeSSEEvent(w, "response.in_progress", shared.Map{"type": "response.in_progress", "response": shell}); err != nil {
		return
	}
	if flusher != nil {
		flusher.Flush()
	}

	chatPayload["stream"] = true
	if _, ok := chatPayload["stream_options"]; !ok {
		chatPayload["stream_options"] = shared.Map{"include_usage": true}
	}
	textStarted := false
	reasoningStarted := false
	reasoningID := shared.NewID("rs")
	reasoningOutputIndex := 0
	textOutputIndex := 0
	content := ""
	reasoning := ""
	var usage any
	finishReason := ""
	toolCalls := map[int]shared.Map{}

	err := s.upstream.StreamChat(r.Context(), chatPayload, func(chunk shared.Map) error {
		if u := responses.ResponseUsageFromKimi(chunk["usage"]); u != nil {
			usage = u
		}
		choices, ok := chunk["choices"].([]any)
		if !ok || len(choices) == 0 {
			return nil
		}
		choice, _ := choices[0].(map[string]any)
		if fr := shared.StringValue(choice["finish_reason"]); fr != "" {
			finishReason = fr
		}
		delta, _ := choice["delta"].(map[string]any)
		if delta == nil {
			return nil
		}
		if dr := shared.StringValue(delta["reasoning_content"]); dr != "" {
			if !reasoningStarted {
				reasoningStarted = true
				reasoningOutputIndex = 0
				if textStarted {
					reasoningOutputIndex = 1
				} else {
					textOutputIndex = 1
				}
				item := responses.ReasoningSummaryItem("", reasoningID, "in_progress")
				if err := writeSSEEvent(w, "response.output_item.added", shared.Map{"type": "response.output_item.added", "output_index": reasoningOutputIndex, "item": item}); err != nil {
					return err
				}
				part := shared.Map{"type": "summary_text", "text": ""}
				if err := writeSSEEvent(w, "response.reasoning_summary_part.added", shared.Map{"type": "response.reasoning_summary_part.added", "item_id": reasoningID, "output_index": reasoningOutputIndex, "summary_index": 0, "part": part}); err != nil {
					return err
				}
			}
			if err := writeSSEEvent(w, "response.reasoning_summary_text.delta", shared.Map{"type": "response.reasoning_summary_text.delta", "item_id": reasoningID, "output_index": reasoningOutputIndex, "summary_index": 0, "delta": dr}); err != nil {
				return err
			}
			reasoning += dr
		}
		if dt := shared.StringValue(delta["content"]); dt != "" {
			if !textStarted {
				textStarted = true
				if reasoningStarted {
					textOutputIndex = 1
				}
				item := shared.Map{"id": messageID, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}}
				if err := writeSSEEvent(w, "response.output_item.added", shared.Map{"type": "response.output_item.added", "output_index": textOutputIndex, "item": item}); err != nil {
					return err
				}
				part := shared.Map{"type": "output_text", "text": "", "annotations": []any{}}
				if err := writeSSEEvent(w, "response.content_part.added", shared.Map{"type": "response.content_part.added", "item_id": messageID, "output_index": textOutputIndex, "content_index": 0, "part": part}); err != nil {
					return err
				}
			}
			if err := writeSSEEvent(w, "response.output_text.delta", shared.Map{"type": "response.output_text.delta", "item_id": messageID, "output_index": textOutputIndex, "content_index": 0, "delta": dt}); err != nil {
				return err
			}
			content += dt
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rawCalls, ok := delta["tool_calls"].([]any); ok {
			for _, raw := range rawCalls {
				call, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				index := shared.IntValue(call["index"], 0)
				existing := toolCalls[index]
				if existing == nil {
					existing = shared.Map{"type": "function", "function": shared.Map{}}
					toolCalls[index] = existing
				}
				responses.MergeStreamToolCall(existing, call)
			}
		}
		return nil
	})
	if err != nil {
		failed := s.responses.BaseResponse(payload, responseID, createdAt, "failed", nil, "", usage, nil)
		failed["error"] = shared.Map{"code": "server_error", "message": err.Error()}
		_ = writeSSEEvent(w, "response.failed", shared.Map{"type": "response.failed", "response": failed})
		return
	}

	outputItems := []shared.Map{}
	outputIndex := 0
	var reasoningItem shared.Map
	var messageItem shared.Map
	if reasoningStarted {
		reasoningItem = responses.ReasoningSummaryItem(reasoning, reasoningID)
		_ = writeSSEEvent(w, "response.reasoning_summary_text.done", shared.Map{"type": "response.reasoning_summary_text.done", "item_id": reasoningID, "output_index": reasoningOutputIndex, "summary_index": 0, "text": reasoning})
		_ = writeSSEEvent(w, "response.reasoning_summary_part.done", shared.Map{"type": "response.reasoning_summary_part.done", "item_id": reasoningID, "output_index": reasoningOutputIndex, "summary_index": 0, "part": shared.Map{"type": "summary_text", "text": reasoning}})
		_ = writeSSEEvent(w, "response.output_item.done", shared.Map{"type": "response.output_item.done", "output_index": reasoningOutputIndex, "item": shared.PublicItem(reasoningItem)})
	}
	if textStarted {
		messageItem = responses.OutputMessageItem(content, messageID)
		if len(toolCalls) > 0 {
			messageItem["_upstream_turn_id"] = shared.NewID("turn")
		}
		if reasoning != "" {
			messageItem["_upstream_reasoning_content"] = reasoning
		}
		_ = writeSSEEvent(w, "response.output_text.done", shared.Map{"type": "response.output_text.done", "item_id": messageID, "output_index": textOutputIndex, "content_index": 0, "text": content})
		_ = writeSSEEvent(w, "response.content_part.done", shared.Map{"type": "response.content_part.done", "item_id": messageID, "output_index": textOutputIndex, "content_index": 0, "part": shared.Map{"type": "output_text", "text": content, "annotations": []any{}}})
		_ = writeSSEEvent(w, "response.output_item.done", shared.Map{"type": "response.output_item.done", "output_index": textOutputIndex, "item": shared.PublicItem(messageItem)})
	}
	if reasoningStarted && (!textStarted || reasoningOutputIndex < textOutputIndex) {
		outputItems = append(outputItems, reasoningItem)
		outputIndex++
	}
	if textStarted {
		outputItems = append(outputItems, messageItem)
		outputIndex++
	}
	if reasoningStarted && textStarted && reasoningOutputIndex > textOutputIndex {
		outputItems = append(outputItems, reasoningItem)
		outputIndex++
	}
	turnID := ""
	for _, item := range outputItems {
		if id := shared.StringValue(item["_upstream_turn_id"]); id != "" {
			turnID = id
			break
		}
	}
	if turnID == "" && len(toolCalls) > 0 {
		turnID = shared.NewID("turn")
	}
	for i := 0; i < len(toolCalls); i++ {
		call := toolCalls[i]
		if call == nil {
			continue
		}
		callReasoning := ""
		if i == 0 {
			callReasoning = reasoning
		}
		item := responses.UpstreamToolCallToResponseItem(call, toolNameMap, callReasoning, turnID)
		outputItems = append(outputItems, item)
		_ = writeSSEEvent(w, "response.output_item.added", shared.Map{"type": "response.output_item.added", "output_index": outputIndex, "item": shared.PublicItem(item)})
		_ = writeSSEEvent(w, "response.output_item.done", shared.Map{"type": "response.output_item.done", "output_index": outputIndex, "item": shared.PublicItem(item)})
		outputIndex++
	}
	status, incomplete := responses.StatusFromFinishReason(finishReason)
	final := s.responses.BaseResponse(payload, responseID, createdAt, status, outputItems, content, usage, incomplete)
	s.store.SaveResponse(final, prepared.AllItems, outputItems, payload["store"] != false, prepared.ConversationID, prepared.InputItems)
	_ = writeSSEEvent(w, "response.completed", shared.Map{"type": "response.completed", "response": final})
	if flusher != nil {
		flusher.Flush()
	}
}

func (s *Server) readJSON(w http.ResponseWriter, r *http.Request, optional bool) (shared.Map, bool) {
	if optional && r.ContentLength == 0 {
		return shared.Map{}, true
	}
	defer r.Body.Close()
	reader := io.Reader(r.Body)
	if s.cfg.MaxRequestBodyBytes > 0 {
		reader = http.MaxBytesReader(w, r.Body, s.cfg.MaxRequestBodyBytes)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			openAIError(w, http.StatusRequestEntityTooLarge, "Request body is too large", "invalid_request_error", "")
			return nil, false
		}
		openAIError(w, http.StatusBadRequest, "Request body could not be read", "invalid_request_error", "")
		return nil, false
	}
	if s.cfg.DebugLogBody {
		log.Printf("debug body request method=%s path=%s body=%s", r.Method, r.URL.RequestURI(), debuglog.Body(body))
	}
	if optional && len(body) == 0 {
		return shared.Map{}, true
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var payload any
	if err := decoder.Decode(&payload); err != nil {
		if optional && errors.Is(err, io.EOF) {
			return shared.Map{}, true
		}
		openAIError(w, http.StatusBadRequest, "Request body must be JSON", "invalid_request_error", "")
		return nil, false
	}
	result, ok := payload.(map[string]any)
	if !ok {
		openAIError(w, http.StatusBadRequest, "request body must be a JSON object", "invalid_request_error", "")
		return nil, false
	}
	return result, true
}

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

func (s *Server) upstreamError(w http.ResponseWriter, err error) {
	status := statusFromError(err)
	openAIError(w, status, err.Error(), errorTypeForStatus(status), "")
}

func statusFromError(err error) int {
	var upstream kimi.HTTPError
	if errors.As(err, &upstream) {
		return upstream.StatusCode
	}
	return http.StatusBadGateway
}

func errorTypeForStatus(status int) string {
	if status == http.StatusUnauthorized {
		return "authentication_error"
	}
	if status >= 500 {
		return "server_error"
	}
	return "invalid_request_error"
}

func statusForLookupError(err error) int {
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "not found") {
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("write json response: %v", err)
	}
}

func openAIError(w http.ResponseWriter, status int, message, typ, param string) {
	writeJSON(w, status, errorPayload(message, typ))
}

func errorPayload(message, typ string) shared.Map {
	return shared.Map{"error": shared.Map{"message": message, "type": typ, "param": nil, "code": nil}}
}

func methodNotAllowed(w http.ResponseWriter) {
	openAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error", "")
}

func setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
}

func writeSSEEvent(w io.Writer, event string, data any) error {
	return sse.Event(w, event, data)
}

type debugResponseWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func newDebugResponseWriter(w http.ResponseWriter) *debugResponseWriter {
	return &debugResponseWriter{ResponseWriter: w}
}

func (w *debugResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *debugResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	remaining := debuglog.MaxBodyBytes + 1 - w.body.Len()
	if remaining > 0 {
		if len(data) > remaining {
			w.body.Write(data[:remaining])
		} else {
			w.body.Write(data)
		}
	}
	return w.ResponseWriter.Write(data)
}

func (w *debugResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *debugResponseWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *debugResponseWriter) bodyBytes() []byte {
	return w.body.Bytes()
}

func paginationParams(r *http.Request, defaultLimit int, defaultOrder string) (int, string) {
	limit := defaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	order := r.URL.Query().Get("order")
	if order != "asc" && order != "desc" {
		order = defaultOrder
	}
	return limit, order
}

var metadataFilterRe = regexp.MustCompile(`^metadata\[([^\]]+)\]$`)

func metadataFilters(r *http.Request) map[string]string {
	out := map[string]string{}
	for key, values := range r.URL.Query() {
		match := metadataFilterRe.FindStringSubmatch(key)
		if len(match) == 2 && len(values) > 0 {
			out[match[1]] = values[0]
		}
	}
	return out
}

func metadataOrEmpty(value any) any {
	if value == nil {
		return shared.Map{}
	}
	return value
}

func valueOrDefault(value any, fallback any) any {
	if value == nil {
		return fallback
	}
	return value
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
