// Package openai is a thin OpenAI Chat Completions client - generic
// request/response plumbing, no knowledge of what it's being used for.
// Not specific to app/lib/dictionary; any future feature needing a chat
// completion (translations, summaries, etc.) can depend on this directly.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/Karan0009/wordotron_api/app/config"
)

// ChatMessage is one turn in a Chat Completions conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ResponseFormat constrains how the model must format its reply. JSONObject
// forces a single valid JSON object as the response content.
type ResponseFormat struct {
	Type string `json:"type"`
}

// JSONObjectResponseFormat is the response_format value that makes the model
// return a single JSON object as message content.
var JSONObjectResponseFormat = &ResponseFormat{Type: "json_object"}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []ChatMessage   `json:"messages"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
	Temperature    float64         `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Client is a thin OpenAI Chat Completions client. It knows nothing about
// words or senses - just how to send messages and get a reply back.
type Client struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

// NewClient builds a Client from config. It works with an empty API key, but
// every call will fail with a 401 from the API - check cfg.Enabled() before
// wiring one in.
func NewClient(cfg config.OpenAI) *Client {
	return &Client{
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		baseURL: cfg.BaseURL,
		http:    &http.Client{Timeout: cfg.Timeout},
	}
}

// ChatCompletion sends messages to the model and returns the assistant
// reply's raw content. temperature follows OpenAI's convention: 0 is
// deterministic, higher is more varied.
func (c *Client) ChatCompletion(ctx context.Context, messages []ChatMessage, format *ResponseFormat, temperature float64) (string, error) {
	reqBody := chatRequest{
		Model:          c.model,
		Messages:       messages,
		ResponseFormat: format,
		Temperature:    temperature,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("call openai: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read openai response: %w", err)
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("decode openai response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if parsed.Error != nil {
			return "", fmt.Errorf("openai error (%d): %s", resp.StatusCode, parsed.Error.Message)
		}
		return "", fmt.Errorf("openai error (%d): %s", resp.StatusCode, raw)
	}

	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("openai response had no choices")
	}

	return parsed.Choices[0].Message.Content, nil
}
