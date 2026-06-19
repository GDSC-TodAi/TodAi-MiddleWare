package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client calls an HTTP-based TTS service.
// Expected contract: POST /synthesize {"text": "..."} → raw PCM audio bytes
type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: timeout},
	}
}

func (c *Client) Enabled() bool { return c.baseURL != "" }

type synthesizeRequest struct {
	Text string `json:"text"`
}

// Synthesize converts text to PCM 16-bit 16kHz mono audio bytes.
func (c *Client) Synthesize(ctx context.Context, text string) ([]byte, error) {
	if !c.Enabled() {
		return nil, nil
	}

	body, err := json.Marshal(synthesizeRequest{Text: text})
	if err != nil {
		return nil, fmt.Errorf("tts marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/synthesize", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tts request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tts call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tts returned status %d", resp.StatusCode)
	}

	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("tts read: %w", err)
	}

	return audio, nil
}
