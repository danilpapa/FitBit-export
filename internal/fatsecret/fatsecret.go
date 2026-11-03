package fatsecret

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fitbit-export/internal/config"
)

const (
	apiURL          = "https://platform.fatsecret.com/rest/server.api"
	requestTokenURL = "https://authentication.fatsecret.com/oauth/request_token"
	authorizeURL    = "https://authentication.fatsecret.com/oauth/authorize"
	accessTokenURL  = "https://authentication.fatsecret.com/oauth/access_token"

	monthMethod   = "food_entries.get_month.v2"
	secondsPerDay = 24 * 60 * 60
	maxBodyBytes  = 1 << 20
)

// Day holds the food diary totals for one calendar day.
type Day struct {
	Date         string
	CaloriesKcal float64
	ProteinG     float64
	CarbsG       float64
	FatG         float64
}

type Client struct {
	httpClient *http.Client
	config     config.FatSecret
	urls       endpoints
	now        func() time.Time
	nonce      func() string
}

type endpoints struct {
	api          string
	requestToken string
	authorize    string
	accessToken  string
}

func New(cfg config.FatSecret) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		config:     cfg,
		urls: endpoints{
			api:          apiURL,
			requestToken: requestTokenURL,
			authorize:    authorizeURL,
			accessToken:  accessTokenURL,
		},
		now:   time.Now,
		nonce: randomNonce,
	}
}

// DailyTotals returns diary totals for the days from start up to, but not including, end.
// Days without food entries are left out.
func (c *Client) DailyTotals(ctx context.Context, start time.Time, end time.Time) ([]Day, error) {
	from, to := start.Format(time.DateOnly), end.Format(time.DateOnly)

	var days []Day
	for month := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC); month.Format(time.DateOnly) < to; month = month.AddDate(0, 1, 0) {
		monthDays, err := c.month(ctx, month)
		if err != nil {
			return nil, err
		}
		for _, day := range monthDays {
			if day.Date >= from && day.Date < to {
				days = append(days, day)
			}
		}
	}
	return days, nil
}

func (c *Client) month(ctx context.Context, month time.Time) ([]Day, error) {
	params := url.Values{
		"method":      {monthMethod},
		"date":        {strconv.FormatInt(month.Unix()/secondsPerDay, 10)},
		"format":      {"json"},
		"oauth_token": {c.config.AccessToken},
	}
	body, status, err := c.send(ctx, http.MethodGet, c.urls.api, params, c.config.AccessSecret)
	if err != nil {
		return nil, fmt.Errorf("FatSecret %s: %w", monthMethod, err)
	}

	var response struct {
		Month *struct {
			Day json.RawMessage `json:"day"`
		} `json:"month"`
		Error *struct {
			Code    any    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("FatSecret %s returned HTTP %d: %s", monthMethod, status, snippet(body))
	}
	if response.Error != nil {
		return nil, fmt.Errorf("FatSecret %s error %v: %s", monthMethod, response.Error.Code, response.Error.Message)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("FatSecret %s returned HTTP %d: %s", monthMethod, status, snippet(body))
	}
	if response.Month == nil {
		return nil, nil
	}
	return decodeDays(response.Month.Day)
}

func decodeDays(raw json.RawMessage) ([]Day, error) {
	type monthDay struct {
		DateInt      flexNumber `json:"date_int"`
		Calories     flexNumber `json:"calories"`
		Protein      flexNumber `json:"protein"`
		Carbohydrate flexNumber `json:"carbohydrate"`
		Fat          flexNumber `json:"fat"`
	}

	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var entries []monthDay
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, fmt.Errorf("decode FatSecret days: %w", err)
		}
	} else {
		var entry monthDay
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, fmt.Errorf("decode FatSecret day: %w", err)
		}
		entries = []monthDay{entry}
	}

	days := make([]Day, len(entries))
	for i, entry := range entries {
		days[i] = Day{
			Date:         time.Unix(int64(entry.DateInt)*secondsPerDay, 0).UTC().Format(time.DateOnly),
			CaloriesKcal: float64(entry.Calories),
			ProteinG:     float64(entry.Protein),
			CarbsG:       float64(entry.Carbohydrate),
			FatG:         float64(entry.Fat),
		}
	}
	return days, nil
}

// flexNumber accepts both JSON numbers and numbers sent as strings.
type flexNumber float64

func (n *flexNumber) UnmarshalJSON(data []byte) error {
	text := strings.Trim(string(data), `"`)
	if text == "" || text == "null" {
		*n = 0
		return nil
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return err
	}
	*n = flexNumber(value)
	return nil
}

func (c *Client) send(
	ctx context.Context,
	method string,
	endpoint string,
	params url.Values,
	tokenSecret string,
) ([]byte, int, error) {
	c.sign(method, endpoint, params, tokenSecret)
	encoded := encodeParams(params)

	target, body := endpoint+"?"+encoded, io.Reader(nil)
	if method == http.MethodPost {
		target, body = endpoint, strings.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, 0, err
	}
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()

	content, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes))
	if err != nil {
		return nil, 0, fmt.Errorf("read response: %w", err)
	}
	return content, response.StatusCode, nil
}

func snippet(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 200 {
		text = text[:200] + "..."
	}
	return text
}
