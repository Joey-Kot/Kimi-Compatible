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

package state

import (
	"sync"

	"kimi-compatible/internal/adapters/openai/shared"
)

type Store struct {
	mu sync.RWMutex

	Responses              map[string]shared.Map
	ResponseInputItems     map[string][]shared.Map
	ResponseContextItems   map[string][]shared.Map
	ChatCompletions        map[string]shared.Map
	ChatCompletionMessages map[string][]shared.Map
	Conversations          map[string]shared.Map
	ConversationItems      map[string][]shared.Map
	ItemsByID              map[string]shared.Map
}

func New() *Store {
	return &Store{
		Responses:              map[string]shared.Map{},
		ResponseInputItems:     map[string][]shared.Map{},
		ResponseContextItems:   map[string][]shared.Map{},
		ChatCompletions:        map[string]shared.Map{},
		ChatCompletionMessages: map[string][]shared.Map{},
		Conversations:          map[string]shared.Map{},
		ConversationItems:      map[string][]shared.Map{},
		ItemsByID:              map[string]shared.Map{},
	}
}

func (s *Store) RegisterItems(items []shared.Map) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registerItemsLocked(items)
}

func (s *Store) registerItemsLocked(items []shared.Map) {
	for _, item := range items {
		id := shared.StringValue(item["id"])
		if id != "" {
			s.ItemsByID[id] = shared.CloneMap(item)
		}
	}
}

func (s *Store) Item(id string) (shared.Map, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.ItemsByID[id]
	return shared.CloneMap(item), ok
}

func (s *Store) Response(id string) (shared.Map, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.Responses[id]
	return shared.CloneMap(item), ok
}

func (s *Store) SaveResponse(response shared.Map, contextItems, outputItems []shared.Map, store bool, conversationID string, currentInputItems []shared.Map) {
	s.mu.Lock()
	defer s.mu.Unlock()
	responseID := shared.StringValue(response["id"])
	if store {
		full := append(shared.CloneSlice(contextItems), shared.CloneSlice(outputItems)...)
		s.ResponseContextItems[responseID] = full
		s.ResponseInputItems[responseID] = shared.CloneSlice(contextItems)
		s.registerItemsLocked(contextItems)
		s.registerItemsLocked(outputItems)
		s.Responses[responseID] = shared.CloneMap(response)
	}
	if conversationID != "" {
		items := s.ConversationItems[conversationID]
		items = append(items, shared.CloneSlice(currentInputItems)...)
		items = append(items, shared.CloneSlice(outputItems)...)
		s.ConversationItems[conversationID] = items
		s.registerItemsLocked(items)
	}
}

func (s *Store) DeleteResponse(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.Responses[id]; !ok {
		return false
	}
	items := append(shared.CloneSlice(s.ResponseInputItems[id]), s.ResponseContextItems[id]...)
	delete(s.Responses, id)
	delete(s.ResponseInputItems, id)
	delete(s.ResponseContextItems, id)
	s.deleteUnreferencedItemsLocked(items)
	return true
}

func (s *Store) ResponseInput(id string) ([]shared.Map, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items, ok := s.ResponseInputItems[id]
	return shared.CloneSlice(items), ok
}

func (s *Store) ResponseContext(id string) ([]shared.Map, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items, ok := s.ResponseContextItems[id]
	return shared.CloneSlice(items), ok
}

func (s *Store) UpdateResponse(id string, fn func(shared.Map) shared.Map) (shared.Map, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.Responses[id]
	if !ok {
		return nil, false
	}
	updated := fn(shared.CloneMap(item))
	s.Responses[id] = shared.CloneMap(updated)
	return shared.CloneMap(updated), true
}

func (s *Store) SaveChatCompletion(completion shared.Map, messages []shared.Map) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := shared.StringValue(completion["id"])
	s.ChatCompletions[id] = shared.CloneMap(completion)
	s.ChatCompletionMessages[id] = shared.CloneSlice(messages)
}

func (s *Store) ChatCompletion(id string) (shared.Map, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.ChatCompletions[id]
	return shared.CloneMap(item), ok
}

func (s *Store) ChatCompletionMessagesFor(id string) ([]shared.Map, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.ChatCompletions[id]; !ok {
		return nil, false
	}
	return shared.CloneSlice(s.ChatCompletionMessages[id]), true
}

func (s *Store) ListChatCompletions(model string, metadata map[string]string) []shared.Map {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []shared.Map{}
	for _, completion := range s.ChatCompletions {
		if model != "" && shared.StringValue(completion["model"]) != model {
			continue
		}
		if !matchesMetadata(completion, metadata) {
			continue
		}
		out = append(out, shared.CloneMap(completion))
	}
	shared.SortByCreatedThenID(out)
	return out
}

func (s *Store) UpdateChatCompletion(id string, metadata any) (shared.Map, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	completion, ok := s.ChatCompletions[id]
	if !ok {
		return nil, false
	}
	completion = shared.CloneMap(completion)
	if metadata == nil {
		completion["metadata"] = shared.Map{}
	} else {
		completion["metadata"] = metadata
	}
	s.ChatCompletions[id] = shared.CloneMap(completion)
	return shared.CloneMap(completion), true
}

func (s *Store) DeleteChatCompletion(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.ChatCompletions[id]; !ok {
		return false
	}
	delete(s.ChatCompletions, id)
	delete(s.ChatCompletionMessages, id)
	return true
}

func (s *Store) SaveConversation(conversation shared.Map, items []shared.Map) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := shared.StringValue(conversation["id"])
	s.Conversations[id] = shared.CloneMap(conversation)
	s.ConversationItems[id] = shared.CloneSlice(items)
	s.registerItemsLocked(items)
}

func (s *Store) Conversation(id string) (shared.Map, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.Conversations[id]
	return shared.CloneMap(item), ok
}

func (s *Store) ConversationItemsFor(id string) ([]shared.Map, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.Conversations[id]; !ok {
		return nil, false
	}
	return shared.CloneSlice(s.ConversationItems[id]), true
}

func (s *Store) UpdateConversation(id string, metadata any) (shared.Map, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	conversation, ok := s.Conversations[id]
	if !ok {
		return nil, false
	}
	conversation = shared.CloneMap(conversation)
	if metadata == nil {
		conversation["metadata"] = shared.Map{}
	} else {
		conversation["metadata"] = metadata
	}
	s.Conversations[id] = shared.CloneMap(conversation)
	return shared.CloneMap(conversation), true
}

func (s *Store) DeleteConversation(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.Conversations[id]; !ok {
		return false
	}
	items := shared.CloneSlice(s.ConversationItems[id])
	delete(s.Conversations, id)
	delete(s.ConversationItems, id)
	s.deleteUnreferencedItemsLocked(items)
	return true
}

func (s *Store) deleteUnreferencedItemsLocked(items []shared.Map) {
	seen := map[string]bool{}
	for _, item := range items {
		id := shared.StringValue(item["id"])
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if s.itemReferencedLocked(id) {
			continue
		}
		delete(s.ItemsByID, id)
	}
}

func (s *Store) itemReferencedLocked(id string) bool {
	for _, items := range s.ResponseInputItems {
		if sliceHasItemID(items, id) {
			return true
		}
	}
	for _, items := range s.ResponseContextItems {
		if sliceHasItemID(items, id) {
			return true
		}
	}
	for _, items := range s.ConversationItems {
		if sliceHasItemID(items, id) {
			return true
		}
	}
	return false
}

func sliceHasItemID(items []shared.Map, id string) bool {
	for _, item := range items {
		if shared.StringValue(item["id"]) == id {
			return true
		}
	}
	return false
}

func matchesMetadata(item shared.Map, filters map[string]string) bool {
	if len(filters) == 0 {
		return true
	}
	metadata, ok := item["metadata"].(map[string]any)
	if !ok {
		return false
	}
	for key, value := range filters {
		if shared.StringValue(metadata[key]) != value {
			return false
		}
	}
	return true
}
