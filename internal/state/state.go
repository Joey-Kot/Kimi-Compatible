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

	limits Limits

	Responses              map[string]shared.Map
	ResponseInputItems     map[string][]shared.Map
	ResponseContextItems   map[string][]shared.Map
	ChatCompletions        map[string]shared.Map
	ChatCompletionMessages map[string][]shared.Map
	Conversations          map[string]shared.Map
	ConversationItems      map[string][]shared.Map
	ItemsByID              map[string]shared.Map

	responseOrder       []string
	chatCompletionOrder []string
	conversationOrder   []string
	itemRefCount        map[string]int
}

type Limits struct {
	MaxResponses       int
	MaxChatCompletions int
	MaxConversations   int
}

type Stats struct {
	Responses       int `json:"responses"`
	ChatCompletions int `json:"chat_completions"`
	Conversations   int `json:"conversations"`
	Items           int `json:"items"`
}

func New() *Store {
	return NewWithLimits(Limits{})
}

func NewWithLimits(limits Limits) *Store {
	return &Store{
		limits:                 limits,
		Responses:              map[string]shared.Map{},
		ResponseInputItems:     map[string][]shared.Map{},
		ResponseContextItems:   map[string][]shared.Map{},
		ChatCompletions:        map[string]shared.Map{},
		ChatCompletionMessages: map[string][]shared.Map{},
		Conversations:          map[string]shared.Map{},
		ConversationItems:      map[string][]shared.Map{},
		ItemsByID:              map[string]shared.Map{},
		itemRefCount:           map[string]int{},
	}
}

func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Stats{
		Responses:       len(s.Responses),
		ChatCompletions: len(s.ChatCompletions),
		Conversations:   len(s.Conversations),
		Items:           len(s.ItemsByID),
	}
}

func (s *Store) RegisterItems(items []shared.Map) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registerItemsLocked(items)
	s.addItemRefsLocked(items)
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
		s.deleteResponseLocked(responseID)
		full := append(shared.CloneSlice(contextItems), shared.CloneSlice(outputItems)...)
		input := shared.CloneSlice(contextItems)
		s.ResponseContextItems[responseID] = full
		s.ResponseInputItems[responseID] = input
		s.registerItemsLocked(full)
		s.addItemRefsLocked(full)
		s.addItemRefsLocked(input)
		s.Responses[responseID] = shared.CloneMap(response)
		s.responseOrder = rememberID(s.responseOrder, responseID)
		s.evictResponsesLocked()
	}
	if conversationID != "" {
		newItems := append(shared.CloneSlice(currentInputItems), shared.CloneSlice(outputItems)...)
		items := s.ConversationItems[conversationID]
		items = append(items, newItems...)
		s.ConversationItems[conversationID] = items
		s.registerItemsLocked(newItems)
		s.addItemRefsLocked(newItems)
	}
}

func (s *Store) DeleteResponse(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.Responses[id]; !ok {
		return false
	}
	s.deleteResponseLocked(id)
	s.responseOrder = forgetID(s.responseOrder, id)
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
	s.chatCompletionOrder = rememberID(s.chatCompletionOrder, id)
	s.evictChatCompletionsLocked()
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
	s.deleteChatCompletionLocked(id)
	s.chatCompletionOrder = forgetID(s.chatCompletionOrder, id)
	return true
}

func (s *Store) SaveConversation(conversation shared.Map, items []shared.Map) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := shared.StringValue(conversation["id"])
	s.deleteConversationLocked(id)
	s.Conversations[id] = shared.CloneMap(conversation)
	s.ConversationItems[id] = shared.CloneSlice(items)
	s.registerItemsLocked(items)
	s.addItemRefsLocked(items)
	s.conversationOrder = rememberID(s.conversationOrder, id)
	s.evictConversationsLocked()
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
	s.deleteConversationLocked(id)
	s.conversationOrder = forgetID(s.conversationOrder, id)
	return true
}

func (s *Store) evictResponsesLocked() {
	if s.limits.MaxResponses <= 0 {
		return
	}
	for len(s.Responses) > s.limits.MaxResponses && len(s.responseOrder) > 0 {
		id := s.responseOrder[0]
		s.responseOrder = s.responseOrder[1:]
		s.deleteResponseLocked(id)
	}
}

func (s *Store) evictChatCompletionsLocked() {
	if s.limits.MaxChatCompletions <= 0 {
		return
	}
	for len(s.ChatCompletions) > s.limits.MaxChatCompletions && len(s.chatCompletionOrder) > 0 {
		id := s.chatCompletionOrder[0]
		s.chatCompletionOrder = s.chatCompletionOrder[1:]
		s.deleteChatCompletionLocked(id)
	}
}

func (s *Store) evictConversationsLocked() {
	if s.limits.MaxConversations <= 0 {
		return
	}
	for len(s.Conversations) > s.limits.MaxConversations && len(s.conversationOrder) > 0 {
		id := s.conversationOrder[0]
		s.conversationOrder = s.conversationOrder[1:]
		s.deleteConversationLocked(id)
	}
}

func (s *Store) deleteResponseLocked(id string) {
	if _, ok := s.Responses[id]; !ok {
		return
	}
	inputItems := shared.CloneSlice(s.ResponseInputItems[id])
	contextItems := shared.CloneSlice(s.ResponseContextItems[id])
	delete(s.Responses, id)
	delete(s.ResponseInputItems, id)
	delete(s.ResponseContextItems, id)
	s.releaseItemRefsLocked(inputItems)
	s.releaseItemRefsLocked(contextItems)
}

func (s *Store) deleteChatCompletionLocked(id string) {
	delete(s.ChatCompletions, id)
	delete(s.ChatCompletionMessages, id)
}

func (s *Store) deleteConversationLocked(id string) {
	if _, ok := s.Conversations[id]; !ok {
		return
	}
	items := shared.CloneSlice(s.ConversationItems[id])
	delete(s.Conversations, id)
	delete(s.ConversationItems, id)
	s.releaseItemRefsLocked(items)
}

func (s *Store) addItemRefsLocked(items []shared.Map) {
	seen := map[string]bool{}
	for _, item := range items {
		id := shared.StringValue(item["id"])
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		s.itemRefCount[id]++
	}
}

func (s *Store) releaseItemRefsLocked(items []shared.Map) {
	seen := map[string]bool{}
	for _, item := range items {
		id := shared.StringValue(item["id"])
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if s.itemRefCount[id] <= 1 {
			delete(s.itemRefCount, id)
			delete(s.ItemsByID, id)
			continue
		}
		s.itemRefCount[id]--
	}
}

func rememberID(order []string, id string) []string {
	if id == "" {
		return order
	}
	order = forgetID(order, id)
	return append(order, id)
}

func forgetID(order []string, id string) []string {
	if id == "" {
		return order
	}
	for i, value := range order {
		if value == id {
			copy(order[i:], order[i+1:])
			return order[:len(order)-1]
		}
	}
	return order
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
