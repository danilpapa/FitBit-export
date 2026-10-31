package googlehealth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"fitbit-export/internal/httpclient"
)

const (
	baseURL            = "https://health.googleapis.com/v4"
	maxRollupPageSize  = 90
	dailyRollupWindows = 1
)

type Client struct {
	http *httpclient.Client
}

func New(http *httpclient.Client) *Client {
	return &Client{http: http}
}

// DailyRollUp returns one aggregate per civil day in the closed-open range.
func (c *Client) DailyRollUp(
	ctx context.Context,
	dataType string,
	start time.Time,
	end time.Time,
) ([]json.RawMessage, error) {
	if !start.Before(end) {
		return nil, fmt.Errorf("start must be before end")
	}

	body, err := json.Marshal(newDailyRollupRequest(start, end))
	if err != nil {
		return nil, fmt.Errorf("encode %s rollup request: %w", dataType, err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		baseURL+"/users/me/dataTypes/"+dataType+"/dataPoints:dailyRollUp",
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

func newDailyRollupRequest(start time.Time, end time.Time) dailyRollupRequest {
	return dailyRollupRequest{
		Range:          civilRange{Start: newCivilTime(start), End: newCivilTime(end)},
		WindowSizeDays: dailyRollupWindows,
		PageSize:       maxRollupPageSize,
	}
}

func newCivilTime(day time.Time) civilTime {
	return civilTime{Date: civilDate{Year: day.Year(), Month: int(day.Month()), Day: day.Day()}}
}

type dailyRollupRequest struct {
	Range          civilRange `json:"range"`
	WindowSizeDays int        `json:"windowSizeDays"`
	PageSize       int        `json:"pageSize"`
}

type civilRange struct {
	Start civilTime `json:"start"`
	End   civilTime `json:"end"`
}

type civilTime struct {
	Date civilDate `json:"date"`
}

type civilDate struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}
