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
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"kimi-compatible/internal/adapters/openai/shared"
	"kimi-compatible/internal/debuglog"
)

type Client struct {
	BaseURL      string
	APIKey       string
	Timeout      time.Duration
	DebugLogBody bool
	HTTPClient   *http.Client
}

type TransportConfig struct {
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	MaxConnsPerHost     int
}

func DefaultTransportConfig() TransportConfig {
	return TransportConfig{
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 100,
		MaxConnsPerHost:     0,
	}
}

func New(baseURL, apiKey string, timeout time.Duration, verifySSL bool) *Client {
	return NewWithTransportConfig(baseURL, apiKey, timeout, verifySSL, DefaultTransportConfig())
}

func NewWithTransportConfig(baseURL, apiKey string, timeout time.Duration, verifySSL bool, transportConfig TransportConfig) *Client {
	return &Client{BaseURL: baseURL, APIKey: apiKey, Timeout: timeout, HTTPClient: newHTTPClient(timeout, verifySSL, transportConfig)}
}

func (c *Client) Chat(ctx context.Context, payload shared.Map) (shared.Map, error) {
	req, err := c.newPostRequest(ctx, c.chatURL(), payload)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.DebugLogBody {
		log.Printf("debug body kimi request url=%s body=%s", req.URL.String(), debuglog.MarshalBody(payload))
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("external Kimi request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if c.DebugLogBody {
		log.Printf("debug body kimi response status=%d body=%s", resp.StatusCode, debuglog.Body(body))
	}
	var data shared.Map
	_ = json.Unmarshal(body, &data)
	if resp.StatusCode >= 400 {
		return nil, HTTPError{StatusCode: resp.StatusCode, Message: kimiErrorMessage(data, string(body))}
	}
	if data == nil {
		return nil, HTTPError{StatusCode: http.StatusBadGateway, Message: "Kimi returned a non-JSON response"}
	}
	return data, nil
}

func (c *Client) StreamChat(ctx context.Context, payload shared.Map, handle func(shared.Map) error) error {
	req, err := c.newPostRequest(ctx, c.chatURL(), payload)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if c.DebugLogBody {
		log.Printf("debug body kimi stream request url=%s body=%s", req.URL.String(), debuglog.MarshalBody(payload))
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("external Kimi stream failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		if c.DebugLogBody {
			log.Printf("debug body kimi stream error status=%d body=%s", resp.StatusCode, debuglog.Body(body))
		}
		var data shared.Map
		_ = json.Unmarshal(body, &data)
		return HTTPError{StatusCode: resp.StatusCode, Message: kimiErrorMessage(data, string(body))}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		text := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if c.DebugLogBody {
			log.Printf("debug body kimi stream response data=%s", debuglog.Body([]byte(text)))
		}
		if text == "[DONE]" {
			break
		}
		var chunk shared.Map
		if err := json.Unmarshal([]byte(text), &chunk); err != nil {
			continue
		}
		if err := handle(chunk); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (c *Client) EstimateTokens(ctx context.Context, payload shared.Map) (shared.Map, error) {
	req, err := c.newPostRequest(ctx, c.tokenEstimateURL(), payload)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.DebugLogBody {
		log.Printf("debug body kimi token request url=%s body=%s", req.URL.String(), debuglog.MarshalBody(payload))
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("external Kimi token estimate failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if c.DebugLogBody {
		log.Printf("debug body kimi token response status=%d body=%s", resp.StatusCode, debuglog.Body(body))
	}
	var data shared.Map
	_ = json.Unmarshal(body, &data)
	if resp.StatusCode >= 400 {
		return nil, HTTPError{StatusCode: resp.StatusCode, Message: kimiErrorMessage(data, string(body))}
	}
	if data == nil {
		return nil, HTTPError{StatusCode: http.StatusBadGateway, Message: "Kimi returned a non-JSON token estimate response"}
	}
	return data, nil
}

func (c *Client) CloseIdleConnections() {
	if c.HTTPClient != nil {
		c.HTTPClient.CloseIdleConnections()
	}
}

func (c *Client) newPostRequest(ctx context.Context, url string, payload shared.Map) (*http.Request, error) {
	if c.APIKey == "" {
		return nil, HTTPError{StatusCode: http.StatusInternalServerError, Message: "Kimi API key is not configured"}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (c *Client) chatURL() string {
	return c.endpointURL("/chat/completions")
}

func (c *Client) tokenEstimateURL() string {
	return c.endpointURL("/tokenizers/estimate-token-count")
}

func (c *Client) endpointURL(path string) string {
	base := strings.TrimRight(c.BaseURL, "/")
	if strings.HasSuffix(base, path) {
		return base
	}
	if strings.HasSuffix(base, "/v1") {
		return base + path
	}
	return base + "/v1" + path
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: c.Timeout}
}

func newHTTPClient(timeout time.Duration, verifySSL bool, config TransportConfig) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if config.MaxIdleConns > 0 {
		transport.MaxIdleConns = config.MaxIdleConns
	}
	if config.MaxIdleConnsPerHost > 0 {
		transport.MaxIdleConnsPerHost = config.MaxIdleConnsPerHost
	}
	if config.MaxConnsPerHost > 0 {
		transport.MaxConnsPerHost = config.MaxConnsPerHost
	}
	if !verifySSL {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &http.Client{Timeout: timeout, Transport: transport}
}

type HTTPError struct {
	StatusCode int
	Message    string
}

func (e HTTPError) Error() string {
	return e.Message
}

func kimiErrorMessage(data shared.Map, fallback string) string {
	if data != nil {
		if errObj, ok := data["error"].(map[string]any); ok {
			if message := shared.StringValue(errObj["message"]); message != "" {
				return "External Kimi error: " + message
			}
		}
		if message := shared.StringValue(data["message"]); message != "" {
			return "External Kimi error: " + message
		}
		if code := shared.StringValue(data["code"]); code != "" {
			return "External Kimi error: " + code
		}
	}
	return "External Kimi error: " + fallback
}
