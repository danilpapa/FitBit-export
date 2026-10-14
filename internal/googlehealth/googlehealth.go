package googlehealth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"fitbit-export/internal/httpclient"
)

const baseURL = "https://health.googleapis.com/v4"

type Client struct {
	http *httpclient.Client
}

type StepsReport struct {
	Start time.Time
	End   time.Time
	Total int64
}

func New(http *httpclient.Client) *Client {
	return &Client{http: http}
}

func (c *Client) Steps(
	ctx context.Context,
	start time.Time,
	end time.Time,
) (StepsReport, error) {
	if !start.Before(end) {
		return StepsReport{}, fmt.Errorf("start must be before end")
	}

	body, err := json.Marshal(stepsRollupRequest{
		Range: timeRange{
			StartTime: start.UTC().Format(time.RFC3339Nano),
			EndTime:   end.UTC().Format(time.RFC3339Nano),
		},
		WindowSize: "86400s",
	})
	if err != nil {
		return StepsReport{}, fmt.Errorf("encode steps request: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		baseURL+"/users/me/dataTypes/steps/dataPoints:rollUp",
		bytes.NewReader(body),
	)
	if err != nil {
		return StepsReport{}, fmt.Errorf("create steps request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return StepsReport{}, fmt.Errorf("get steps: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return StepsReport{}, fmt.Errorf("Google Health API returned HTTP %d", response.StatusCode)
	}

	var result stepsRollupResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return StepsReport{}, fmt.Errorf("decode steps response: %w", err)
	}

	total, err := totalSteps(result.RollupDataPoints)
	if err != nil {
		return StepsReport{}, err
	}

	return StepsReport{Start: start, End: end, Total: total}, nil
}

func totalSteps(points []stepsRollupDataPoint) (int64, error) {
	var total int64

	for _, point := range points {
		if point.Steps == nil {
			continue
		}

		count, err := strconv.ParseInt(point.Steps.CountSum, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse steps count: %w", err)
		}
		total += count
	}

	return total, nil
}

type stepsRollupRequest struct {
	Range      timeRange `json:"range"`
	WindowSize string    `json:"windowSize"`
}

type timeRange struct {
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
}

type stepsRollupResponse struct {
	RollupDataPoints []stepsRollupDataPoint `json:"rollupDataPoints"`
}

type stepsRollupDataPoint struct {
	Steps *struct {
		CountSum string `json:"countSum"`
	} `json:"steps"`
}
