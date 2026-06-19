package adk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Hyuk-II/todai-middleware/pkg/model"
)

// MetricsResult holds the five welfare indicators returned by Google ADK.
type MetricsResult struct {
	SocialIsolation float64 `json:"social_isolation"` // 사회적 고립
	HealthAnxiety   float64 `json:"health_anxiety"`   // 건강 불안
	DailyVitality   float64 `json:"daily_vitality"`   // 일상 활력
	EmotionVariance float64 `json:"emotion_variance"` // 감정 변동
	CognitiveLoad   float64 `json:"cognitive_load"`   // 인지 부하
}

// Client sends emotion + STT results to the Google ADK agent and retrieves five welfare metrics.
// Expected contract: POST /analyze {"emotion": {...}, "text": "..."} → MetricsResult JSON
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

func (c *Client) Enabled() bool          { return c.baseURL != "" }
func (c *Client) Timeout() time.Duration { return c.http.Timeout }

type analyzeRequest struct {
	Emotion model.EmotionResult `json:"emotion"`
	Text    string              `json:"text"`
}

// Analyze sends emotion distribution and transcribed text to ADK
// and returns the five welfare indicator scores.
func (c *Client) Analyze(ctx context.Context, emotion model.EmotionResult, text string) (MetricsResult, error) {
	if !c.Enabled() {
		return MetricsResult{}, fmt.Errorf("adk client not configured")
	}

	body, err := json.Marshal(analyzeRequest{Emotion: emotion, Text: text})
	if err != nil {
		return MetricsResult{}, fmt.Errorf("adk marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/analyze", bytes.NewReader(body))
	if err != nil {
		return MetricsResult{}, fmt.Errorf("adk request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return MetricsResult{}, fmt.Errorf("adk call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return MetricsResult{}, fmt.Errorf("adk returned status %d", resp.StatusCode)
	}

	var result MetricsResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return MetricsResult{}, fmt.Errorf("adk decode: %w", err)
	}

	return result, nil
}
