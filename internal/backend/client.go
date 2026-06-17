package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxErrorBodyBytes = 4096

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.baseURL != ""
}

func (c *Client) CreateAnalysisJob(
	ctx context.Context,
	req CreateAnalysisJobRequest,
) (CreateAnalysisJobResponse, error) {
	var response CreateAnalysisJobResponse
	if !c.Enabled() {
		return response, fmt.Errorf("backend integration is disabled")
	}

	if err := c.doJSON(ctx, http.MethodPost, "/internal/analysis-jobs", req, &response); err != nil {
		return response, err
	}
	if response.JobID == "" {
		return response, fmt.Errorf("create analysis job: backend returned empty job_id")
	}
	return response, nil
}

func (c *Client) CreateJobEvent(
	ctx context.Context,
	jobID string,
	req CreateJobEventRequest,
) error {
	path := "/internal/analysis-jobs/" + url.PathEscape(jobID) + "/events"
	return c.doJSON(ctx, http.MethodPost, path, req, nil)
}

func (c *Client) UpdateJobStatus(
	ctx context.Context,
	jobID string,
	req UpdateJobStatusRequest,
) error {
	path := "/internal/analysis-jobs/" + url.PathEscape(jobID) + "/status"
	return c.doJSON(ctx, http.MethodPatch, path, req, nil)
}

func (c *Client) doJSON(
	ctx context.Context,
	method string,
	path string,
	requestBody any,
	responseBody any,
) error {
	if !c.Enabled() {
		return fmt.Errorf("backend integration is disabled")
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("marshal backend request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		method,
		c.baseURL+path,
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("create backend request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call backend %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		errorBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		if readErr != nil {
			return fmt.Errorf(
				"backend %s %s returned status %d; read error body: %w",
				method,
				path,
				resp.StatusCode,
				readErr,
			)
		}
		return fmt.Errorf(
			"backend %s %s returned status %d: %s",
			method,
			path,
			resp.StatusCode,
			strings.TrimSpace(string(errorBody)),
		)
	}

	if responseBody == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(responseBody); err != nil {
		return fmt.Errorf("decode backend response: %w", err)
	}
	return nil
}
