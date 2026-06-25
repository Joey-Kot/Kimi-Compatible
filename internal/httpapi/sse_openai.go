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

	"kimi-compatible/internal/adapters/openai/chat"
	"kimi-compatible/internal/adapters/openai/responses"
	"kimi-compatible/internal/adapters/openai/shared"
	"kimi-compatible/internal/sse"
)

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
	var contentBuilder strings.Builder
	var reasoningBuilder strings.Builder
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
			reasoningBuilder.WriteString(dr)
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
			contentBuilder.WriteString(dt)
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
	content := contentBuilder.String()
	reasoning := reasoningBuilder.String()
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
