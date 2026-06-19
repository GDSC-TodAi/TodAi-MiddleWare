package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	DefaultModel = "llama3.2"
	systemPrompt = "당신은 TodAi입니다. 한국 노인분들의 따뜻한 말벗 AI입니다. 상대방이 말한 내용에 공감하며 짧고 따뜻하게 1~2문장으로 답해주세요."
)

// Client calls an Ollama-compatible local LLM service.
// Expected contract: POST /api/generate → {"response": "..."}
type Client struct {
	baseURL string
	model   string
	http    *http.Client
}

func NewClient(baseURL, model string, timeout time.Duration) *Client {
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		baseURL: baseURL,
		model:   model,
		http:    &http.Client{Timeout: timeout},
	}
}

func (c *Client) Enabled() bool { return c.baseURL != "" }

type generateRequest struct {
	Model  string `json:"model"`
	System string `json:"system"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type generateResponse struct {
	Response string `json:"response"`
}

// Complete sends transcribed user speech to the LLM and returns the assistant reply.
func (c *Client) Complete(ctx context.Context, userText string) (string, error) {
	if !c.Enabled() {
		return "", nil
	}

	body, err := json.Marshal(generateRequest{
		Model:  c.model,
		System: systemPrompt,
		Prompt: userText,
		Stream: false,
	})
	if err != nil {
		return "", fmt.Errorf("llm marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("llm request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm returned status %d", resp.StatusCode)
	}

	var result generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("llm decode: %w", err)
	}

	return result.Response, nil
}
