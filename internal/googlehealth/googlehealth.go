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
	dataTypesURL      = "https://health.googleapis.com/v4/users/me/dataTypes/"
	listPageSize      = "1000"
	maxRollupPageSize = 90
)

type Client struct {
	http *httpclient.Client
}

func New(http *httpclient.Client) *Client {
	return &Client{http: http}
}

func (c *Client) DailyRollUp(
	ctx context.Context,
	dataType string,
	start time.Time,
	end time.Time,
) ([]json.RawMessage, error) {
	body, err := json.Marshal(newDailyRollupRequest(start, end))
	if err != nil {
		return nil, fmt.Errorf("encode %s rollup request: %w", dataType, err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		dataTypesURL+dataType+"/dataPoints:dailyRollUp",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("create %s rollup request: %w", dataType, err)
	}
	request.Header.Set("Content-Type", "application/json")

	var result struct {
		RollupDataPoints []json.RawMessage `json:"rollupDataPoints"`
	}
	if err := c.fetch(request, &result); err != nil {
		return nil, fmt.Errorf("get %s rollup: %w", dataType, err)
	}
	return result.RollupDataPoints, nil
}

func (c *Client) List(ctx context.Context, dataType string, filter string) ([]json.RawMessage, error) {
	var points []json.RawMessage
	query := url.Values{"pageSize": {listPageSize}, "filter": {filter}}

	for {
		page, err := c.listPage(ctx, dataType, query)
		if err != nil {
			return nil, err
		}

		points = append(points, page.DataPoints...)
		if page.NextPageToken == "" {
			return points, nil
		}
		query.Set("pageToken", page.NextPageToken)
	}
}

func (c *Client) Latest(ctx context.Context, dataType string, filter string) (json.RawMessage, error) {
	page, err := c.listPage(ctx, dataType, url.Values{"pageSize": {"1"}, "filter": {filter}})
	if err != nil {
		return nil, err
	}
	if len(page.DataPoints) == 0 {
		return nil, nil
	}
	return page.DataPoints[0], nil
}

func (c *Client) listPage(ctx context.Context, dataType string, query url.Values) (dataPage, error) {
	target := dataTypesURL + dataType + "/dataPoints?" + query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return dataPage{}, fmt.Errorf("create %s list request: %w", dataType, err)
	}

	var page dataPage
	if err := c.fetch(request, &page); err != nil {
		return dataPage{}, fmt.Errorf("get %s data points: %w", dataType, err)
	}
	return page, nil
}

func (c *Client) fetch(request *http.Request, result any) error {
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Google Health API returned HTTP %d", response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(result)
}

func newDailyRollupRequest(start time.Time, end time.Time) dailyRollupRequest {
	return dailyRollupRequest{
		Range:          civilRange{Start: newCivilTime(start), End: newCivilTime(end)},
		WindowSizeDays: 1,
		PageSize:       maxRollupPageSize,
	}
}

func newCivilTime(day time.Time) civilTime {
	return civilTime{Date: civilDate{Year: day.Year(), Month: int(day.Month()), Day: day.Day()}}
}

type dataPage struct {
	DataPoints    []json.RawMessage `json:"dataPoints"`
	NextPageToken string            `json:"nextPageToken"`
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
