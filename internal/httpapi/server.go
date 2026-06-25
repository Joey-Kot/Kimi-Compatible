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
	"context"
	anthropic "kimi-compatible/internal/adapters/anthropic/messages"
	gemini "kimi-compatible/internal/adapters/gemini/generate"
	"kimi-compatible/internal/adapters/openai/chat"
	"kimi-compatible/internal/adapters/openai/responses"
	"kimi-compatible/internal/adapters/openai/shared"
	"kimi-compatible/internal/config"
	"kimi-compatible/internal/state"
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
