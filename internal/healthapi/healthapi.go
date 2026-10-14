package healthapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"fitbit-export/internal/httpclient"
)

const baseURL = "https://health.googleapis.com/v4"

type Client struct {
	http *httpclient.Client
}

func New(http *httpclient.Client) *Client {
	return &Client{http: http}
}

func (c *Client) ListSteps(
	ctx context.Context,
	start time.Time,
	end time.Time,
) ([]json.RawMessage, error) {
	body, err := json.Marshal(struct {
		Range struct {
			StartTime string `json:"startTime"`
			EndTime   string `json:"endTime"`
		} `json:"range"`
		WindowSize string `json:"windowSize"`
	}{
		Range: struct {
			StartTime string `json:"startTime"`
			EndTime   string `json:"endTime"`
		}{
			StartTime: start.UTC().Format(time.RFC3339Nano),
			EndTime:   end.UTC().Format(time.RFC3339Nano),
		},
		WindowSize: "86400s",
	})
	if err != nil {
		return nil, fmt.Errorf("encode steps rollup request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		baseURL+"/users/me/dataTypes/steps/dataPoints:rollUp",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("create steps rollup request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("roll up steps: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Google Health API returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		RollupDataPoints []json.RawMessage `json:"rollupDataPoints"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode steps rollup response: %w", err)
	}

	return result.RollupDataPoints, nil
}
