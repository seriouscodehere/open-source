// sdk/go/client.go
package ratelimit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	baseURL   string
	apiKey    string
	serviceID string
	client    *http.Client
}

type CheckRequest struct {
	ServiceID string            `json:"service_id"`
	Endpoint  string            `json:"endpoint"`
	IP        string            `json:"ip"`
	UserID    string            `json:"user_id,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

type CheckResponse struct {
	Allowed        bool   `json:"allowed"`
	Blocked        bool   `json:"blocked"`
	BlockRemaining int    `json:"block_remaining_seconds"`
	RetryAfter     int    `json:"retry_after_seconds"`
	Reason         string `json:"reason"`
}

func NewClient(baseURL, apiKey, serviceID string) *Client {
	return &Client{
		baseURL:   baseURL,
		apiKey:    apiKey,
		serviceID: serviceID,
		client:    &http.Client{Timeout: 5 * time.Second}, // Increased timeout
	}
}

func (c *Client) Check(ctx context.Context, endpoint, ip string, headers map[string]string) (*CheckResponse, error) {
	reqBody := CheckRequest{
		ServiceID: c.serviceID,
		Endpoint:  endpoint,
		IP:        ip,
		Headers:   headers,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/check", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rate limiter unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result CheckResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

func (c *Client) IsAllowed(ctx context.Context, endpoint, ip string) (bool, error) {
	result, err := c.Check(ctx, endpoint, ip, nil)
	if err != nil {
		return false, err
	}
	return result.Allowed, nil
}
