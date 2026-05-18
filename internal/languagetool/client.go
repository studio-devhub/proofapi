package languagetool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	BaseURL string
	Timeout time.Duration
}

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(cfg Config) *Client {
	return &Client{
		baseURL: cfg.BaseURL,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

func (c *Client) Check(ctx context.Context, req CheckRequest) (*CheckResponse, error) {
	body := url.Values{
		"text":        {req.Text},
		"language":    {req.Language},
		"level":       {req.Level},
		"enabledOnly": {"false"},
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		c.baseURL+"/v2/check",
		strings.NewReader(body.Encode()),
	)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("lt request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lt responded %d", resp.StatusCode)
	}

	var result struct {
		Matches  []Match  `json:"matches"`
		Language Language `json:"language"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode failed: %w", err)
	}

	return &CheckResponse{
		Matches:   result.Matches,
		Language:  result.Language,
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
		Cached:    false,
	}, nil
}

func (c *Client) Languages(ctx context.Context) ([]map[string]any, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v2/languages", nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var langs []map[string]any
	return langs, json.NewDecoder(resp.Body).Decode(&langs)
}

func (c *Client) Ping(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v2/languages", nil)
	if err != nil {
		return false
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
