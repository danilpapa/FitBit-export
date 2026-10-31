package googlehealth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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

func (c *Client) Steps(ctx context.Context, start time.Time, end time.Time) (StepsReport, error) {
	points, err := c.RollUp(ctx, "steps", start, end)
	if err != nil {
		return StepsReport{}, fmt.Errorf("get steps: %w", err)
	}

	total, err := totalSteps(points)
	if err != nil {
		return StepsReport{}, err
	}

	return StepsReport{Start: start, End: end, Total: total}, nil
}

// RollUp returns reconciled aggregate values for one metric in a time range.
func (c *Client) RollUp(
	ctx context.Context,
	dataType string,
	start time.Time,
	end time.Time,
) ([]json.RawMessage, error) {
	if !start.Before(end) {
		return nil, fmt.Errorf("start must be before end")
	}

	body, err := json.Marshal(rollupRequest{
		Range: timeRange{
			StartTime: start.UTC().Format(time.RFC3339Nano),
			EndTime:   end.UTC().Format(time.RFC3339Nano),
		},
		WindowSize: "86400s",
	})
	if err != nil {
		return nil, fmt.Errorf("encode %s rollup request: %w", dataType, err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		baseURL+"/users/me/dataTypes/"+dataType+"/dataPoints:rollUp",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("create %s rollup request: %w", dataType, err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("get %s rollup: %w", dataType, err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("Google Health API returned HTTP %d", response.StatusCode)
	}

	var result struct {
		RollupDataPoints []json.RawMessage `json:"rollupDataPoints"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode %s rollup response: %w", dataType, err)
	}

	return result.RollupDataPoints, nil
}

// List downloads every raw data point matching the supplied API filter.
func (c *Client) List(
	ctx context.Context,
	dataType string,
	filter string,
) ([]json.RawMessage, error) {
	var points []json.RawMessage
	pageToken := ""

	for {
		endpoint, err := url.Parse(baseURL + "/users/me/dataTypes/" + dataType + "/dataPoints")
		if err != nil {
			return nil, fmt.Errorf("parse %s endpoint: %w", dataType, err)
		}

		query := endpoint.Query()
		query.Set("pageSize", "1000")
		query.Set("filter", filter)
		if pageToken != "" {
			query.Set("pageToken", pageToken)
		}
		endpoint.RawQuery = query.Encode()

		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("create %s list request: %w", dataType, err)
		}

		response, err := c.http.Do(request)
		if err != nil {
			return nil, fmt.Errorf("get %s data points: %w", dataType, err)
		}

		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			response.Body.Close()
			return nil, fmt.Errorf("Google Health API returned HTTP %d", response.StatusCode)
		}

		var page struct {
			DataPoints    []json.RawMessage `json:"dataPoints"`
			NextPageToken string            `json:"nextPageToken"`
		}
		err = json.NewDecoder(response.Body).Decode(&page)
		response.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode %s list response: %w", dataType, err)
		}

		points = append(points, page.DataPoints...)
		if page.NextPageToken == "" {
			return points, nil
		}
		pageToken = page.NextPageToken
	}
}

func totalSteps(points []json.RawMessage) (int64, error) {
	var total int64

	for _, raw := range points {
		var point struct {
			Steps *struct {
				CountSum string `json:"countSum"`
			} `json:"steps"`
		}
		if err := json.Unmarshal(raw, &point); err != nil {
			return 0, fmt.Errorf("decode steps rollup point: %w", err)
		}
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

type rollupRequest struct {
	Range      timeRange `json:"range"`
	WindowSize string    `json:"windowSize"`
}

type timeRange struct {
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
}
