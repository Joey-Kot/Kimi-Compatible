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

package kimi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"

	"kimi-compatible/internal/adapters/openai/shared"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestChatPostsJSONAndParsesResponse(t *testing.T) {
	client := New("https://kimi.test", "sk-upstream", time.Second, true)
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://kimi.test/v1/chat/completions" {
			t.Fatalf("url = %s", r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer sk-upstream" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Fatalf("Accept = %q", r.Header.Get("Accept"))
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["model"] != "kimi-k2.7-code" {
			t.Fatalf("payload = %#v", payload)
		}
		return jsonResponse(http.StatusOK, `{"id":"chat_1","choices":[]}`), nil
	})}

	response, err := client.Chat(context.Background(), shared.Map{"model": "kimi-k2.7-code"})
	if err != nil {
		t.Fatal(err)
	}
	if response["id"] != "chat_1" {
		t.Fatalf("response = %#v", response)
	}
}

func TestChatUsesExplicitChatCompletionsURL(t *testing.T) {
	client := New("https://kimi.test/custom/v1/chat/completions", "sk-upstream", time.Second, true)
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://kimi.test/custom/v1/chat/completions" {
			t.Fatalf("url = %s", r.URL.String())
		}
		return jsonResponse(http.StatusOK, `{"id":"chat_1"}`), nil
	})}
	if _, err := client.Chat(context.Background(), shared.Map{}); err != nil {
		t.Fatal(err)
	}
}

func TestEstimateTokensPostsToKimiTokenizer(t *testing.T) {
	client := New("https://kimi.test/v1", "sk-upstream", time.Second, true)
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://kimi.test/v1/tokenizers/estimate-token-count" {
			t.Fatalf("url = %s", r.URL.String())
		}
		return jsonResponse(http.StatusOK, `{"data":{"total_tokens":42}}`), nil
	})}
	response, err := client.EstimateTokens(context.Background(), shared.Map{"model": "kimi-k2.7-code", "messages": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	data := response["data"].(map[string]any)
	if data["total_tokens"] != float64(42) {
		t.Fatalf("response = %#v", response)
	}
}

func TestChatReturnsHTTPErrorMessage(t *testing.T) {
	client := New("https://kimi.test", "sk-upstream", time.Second, true)
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusBadRequest, `{"error":{"message":"bad request"}}`), nil
	})}
	_, err := client.Chat(context.Background(), shared.Map{})
	var httpErr HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HTTPError, got %T %v", err, err)
	}
	if httpErr.StatusCode != http.StatusBadRequest || !strings.Contains(httpErr.Message, "bad request") {
		t.Fatalf("http error = %#v", httpErr)
	}
}

func TestChatRequiresAPIKey(t *testing.T) {
	client := New("https://example.test", "", time.Second, true)
	_, err := client.Chat(context.Background(), shared.Map{})
	var httpErr HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("error = %#v", err)
	}
}

func TestChatRejectsNonJSONSuccess(t *testing.T) {
	client := New("https://kimi.test", "sk-upstream", time.Second, true)
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return textResponse(http.StatusOK, "not-json"), nil
	})}
	_, err := client.Chat(context.Background(), shared.Map{})
	var httpErr HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("error = %#v", err)
	}
}

func TestDebugLogBodyLogsRedactedUpstreamBodies(t *testing.T) {
	var logs bytes.Buffer
	previousOutput := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	defer log.SetOutput(previousOutput)
	defer log.SetFlags(previousFlags)

	client := New("https://kimi.test", "sk-upstream", time.Second, true)
	client.DebugLogBody = true
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"id":"chat_1","choices":[]}`), nil
	})}
	_, err := client.Chat(context.Background(), shared.Map{"model": "kimi-k2.7-code", "api_key": "secret-value"})
	if err != nil {
		t.Fatal(err)
	}
	text := logs.String()
	if !strings.Contains(text, "debug body kimi request") || !strings.Contains(text, "debug body kimi response") {
		t.Fatalf("missing debug logs: %s", text)
	}
	if strings.Contains(text, "secret-value") {
		t.Fatalf("debug logs leaked secret: %s", text)
	}
	if !strings.Contains(text, `"api_key":"[REDACTED]"`) || !strings.Contains(text, `"id":"chat_1"`) {
		t.Fatalf("debug logs = %s", text)
	}
}

func TestNewConfiguresTLSVerification(t *testing.T) {
	verified := New("https://kimi.test", "sk-upstream", time.Second, true)
	if isInsecureSkipVerify(verified.HTTPClient) {
		t.Fatal("expected TLS verification to be enabled by default")
	}

	unverified := New("https://kimi.test", "sk-upstream", time.Second, false)
	if !isInsecureSkipVerify(unverified.HTTPClient) {
		t.Fatal("expected TLS verification to be disabled")
	}
}

func TestNewConfiguresConnectionPool(t *testing.T) {
	client := New("https://kimi.test", "sk-upstream", time.Second, true)
	transport := httpTransport(client.HTTPClient)
	if transport.MaxIdleConns != 200 {
		t.Fatalf("MaxIdleConns = %d", transport.MaxIdleConns)
	}
	if transport.MaxIdleConnsPerHost != 100 {
		t.Fatalf("MaxIdleConnsPerHost = %d", transport.MaxIdleConnsPerHost)
	}
	if transport.MaxConnsPerHost != 0 {
		t.Fatalf("MaxConnsPerHost = %d", transport.MaxConnsPerHost)
	}
}

func TestNewWithTransportConfig(t *testing.T) {
	client := NewWithTransportConfig("https://kimi.test", "sk-upstream", time.Second, true, TransportConfig{
		MaxIdleConns:        20,
		MaxIdleConnsPerHost: 10,
		MaxConnsPerHost:     5,
	})
	transport := httpTransport(client.HTTPClient)
	if transport.MaxIdleConns != 20 {
		t.Fatalf("MaxIdleConns = %d", transport.MaxIdleConns)
	}
	if transport.MaxIdleConnsPerHost != 10 {
		t.Fatalf("MaxIdleConnsPerHost = %d", transport.MaxIdleConnsPerHost)
	}
	if transport.MaxConnsPerHost != 5 {
		t.Fatalf("MaxConnsPerHost = %d", transport.MaxConnsPerHost)
	}
}

func TestStreamChatParsesSSEDataChunks(t *testing.T) {
	client := New("https://kimi.test", "sk-upstream", time.Second, true)
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("Accept = %q", r.Header.Get("Accept"))
		}
		return textResponse(http.StatusOK, strings.Join([]string{
			"event: ignored",
			"data: {\"id\":\"chunk_1\"}",
			"",
			"data: not-json",
			"",
			"data: {\"id\":\"chunk_2\"}",
			"",
			"data: [DONE]",
			"",
		}, "\n")), nil
	})}
	ids := []string{}
	err := client.StreamChat(context.Background(), shared.Map{"stream": true}, func(chunk shared.Map) error {
		ids = append(ids, shared.StringValue(chunk["id"]))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "chunk_1" || ids[1] != "chunk_2" {
		t.Fatalf("ids = %#v", ids)
	}
}

func TestStreamChatStopsWhenHandlerReturnsError(t *testing.T) {
	client := New("https://kimi.test", "sk-upstream", time.Second, true)
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return textResponse(http.StatusOK, "data: {\"id\":\"chunk_1\"}\n\n"), nil
	})}
	want := errors.New("stop")
	err := client.StreamChat(context.Background(), shared.Map{"stream": true}, func(shared.Map) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
}

func isInsecureSkipVerify(client *http.Client) bool {
	if client == nil {
		return false
	}
	transport := httpTransport(client)
	if transport == nil || transport.TLSClientConfig == nil {
		return false
	}
	return transport.TLSClientConfig.InsecureSkipVerify
}

func httpTransport(client *http.Client) *http.Transport {
	if client == nil {
		return nil
	}
	transport, _ := client.Transport.(*http.Transport)
	return transport
}

func jsonResponse(status int, body string) *http.Response {
	resp := textResponse(status, body)
	resp.Header.Set("Content-Type", "application/json")
	return resp
}

func textResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
