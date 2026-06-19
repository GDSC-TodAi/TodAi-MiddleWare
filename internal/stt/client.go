package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client calls an HTTP-based Whisper STT service.
// Expected contract: POST /transcribe with raw PCM bytes → {"text": "..."}
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

type transcribeResponse struct {
	Text string `json:"text"`
}

// Transcribe sends PCM 16-bit 16kHz mono audio to the STT service and returns the transcript.
func (c *Client) Transcribe(ctx context.Context, audio []byte) (string, error) {
	if !c.Enabled() {
		return "", nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/transcribe", bytes.NewReader(audio))
	if err != nil {
		return "", fmt.Errorf("stt request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("stt call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stt returned status %d", resp.StatusCode)
	}

	var result transcribeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("stt decode: %w", err)
	}

	return result.Text, nil
}
