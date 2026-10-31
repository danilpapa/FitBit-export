package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fitbit-export/internal/config"
)

const (
	TokenURL = "https://oauth2.googleapis.com/token"

	maxAttempts = 4
	baseDelay   = time.Second
	maxDelay    = 30 * time.Second
)

type Client struct {
	httpClient  *http.Client
	config      config.Config
	tokenURL    string
	baseDelay   time.Duration
	accessToken string
}

func New(config config.Config) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		config:     config,
		tokenURL:   TokenURL,
		baseDelay:  baseDelay,
	}
}

func (c *Client) Do(request *http.Request) (*http.Response, error) {
	body, err := readBody(request)
	if err != nil {
		return nil, err
	}

	if c.accessToken == "" {
		if err := c.refreshAccessToken(request.Context()); err != nil {
			return nil, err
		}
	}

	response, err := c.sendAuthorized(request, body)
	if err != nil || response.StatusCode != http.StatusUnauthorized {
		return response, err
	}
	response.Body.Close()

	if err := c.refreshAccessToken(request.Context()); err != nil {
		return nil, err
	}
	return c.sendAuthorized(request, body)
}

func (c *Client) PostForm(ctx context.Context, target string, form url.Values) (*http.Response, error) {
	return c.retry(ctx, target, func() (*http.Response, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return c.httpClient.Do(request)
	})
}

func (c *Client) sendAuthorized(original *http.Request, body []byte) (*http.Response, error) {
	return c.retry(original.Context(), original.URL.Path, func() (*http.Response, error) {
		request := original.Clone(original.Context())
		request.Header.Set("Authorization", "Bearer "+c.accessToken)
		request.Body = nil
		if body != nil {
			request.Body = io.NopCloser(bytes.NewReader(body))
			request.ContentLength = int64(len(body))
		}
		return c.httpClient.Do(request)
	})
}

func (c *Client) refreshAccessToken(ctx context.Context) error {
	response, err := c.PostForm(ctx, c.tokenURL, url.Values{
		"client_id":     {c.config.ClientID},
		"client_secret": {c.config.ClientSecret},
		"refresh_token": {c.config.RefreshToken},
		"grant_type":    {"refresh_token"},
	})
	if err != nil {
		return fmt.Errorf("send OAuth request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("OAuth refresh returned HTTP %d", response.StatusCode)
	}

	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil {
		return fmt.Errorf("decode OAuth response: %w", err)
	}
	if token.AccessToken == "" {
		return errors.New("OAuth response has no access_token")
	}

	c.accessToken = token.AccessToken
	return nil
}

func (c *Client) retry(
	ctx context.Context,
	name string,
	attempt func() (*http.Response, error),
) (*http.Response, error) {
	delay := c.baseDelay

	for try := 1; ; try++ {
		response, err := attempt()
		if try == maxAttempts || ctx.Err() != nil || !isTransient(response, err) {
			return response, err
		}

		wait := retryDelay(response, delay)
		if err != nil {
			log.Printf("%s: retrying in %s after: %v", name, wait, err)
		} else {
			log.Printf("%s: retrying in %s after HTTP %d", name, wait, response.StatusCode)
			response.Body.Close()
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		delay *= 2
	}
}

func isTransient(response *http.Response, err error) bool {
	if err != nil {
		return true
	}

	switch response.StatusCode {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

func retryDelay(response *http.Response, fallback time.Duration) time.Duration {
	if response == nil {
		return fallback
	}

	seconds, err := strconv.Atoi(response.Header.Get("Retry-After"))
	if err != nil || seconds <= 0 {
		return fallback
	}
	return min(time.Duration(seconds)*time.Second, maxDelay)
}

func readBody(request *http.Request) ([]byte, error) {
	if request.Body == nil {
		return nil, nil
	}
	defer request.Body.Close()

	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	return body, nil
}
