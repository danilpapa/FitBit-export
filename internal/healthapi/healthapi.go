package healthapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	filter := fmt.Sprintf(
		`steps.interval.start_time >= "%s" AND steps.interval.start_time < "%s"`,
		start.UTC().Format(time.RFC3339),
		end.UTC().Format(time.RFC3339),
	)

	var all []json.RawMessage
	pageToken := ""

	for {
		u, err := url.Parse(
			baseURL + "/users/me/dataTypes/steps/dataPoints",
		)
		if err != nil {
			return nil, err
		}

		query := u.Query()
		query.Set("pageSize", "10000")
		query.Set("filter", filter)

		if pageToken != "" {
			query.Set("pageToken", pageToken)
		}

		u.RawQuery = query.Encode()

		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			u.String(),
			nil,
		)
		if err != nil {
			return nil, fmt.Errorf("create steps request: %w", err)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("get steps: %w", err)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return nil, fmt.Errorf("Google Health API returned HTTP %d", resp.StatusCode)
		}

		var page struct {
			DataPoints    []json.RawMessage `json:"dataPoints"`
			NextPageToken string            `json:"nextPageToken"`
		}

		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()

		if err != nil {
			return nil, fmt.Errorf("decode steps response: %w", err)
		}

		all = append(all, page.DataPoints...)

		if page.NextPageToken == "" {
			break
		}

		pageToken = page.NextPageToken
	}

	return all, nil
}