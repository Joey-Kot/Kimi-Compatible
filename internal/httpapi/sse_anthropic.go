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
	"kimi-compatible/internal/adapters/openai/responses"
	"kimi-compatible/internal/adapters/openai/shared"
)

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
