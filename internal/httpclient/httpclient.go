package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"fitbit-export/internal/config"
)

const (
	oauthTokenURL = "https://oauth2.googleapis.com/token"
	maxAttempts   = 4
	baseDelay     = time.Second
	maxDelay      = 30 * time.Second
)

type Client struct {
	httpClient  *http.Client
	config      config.Config
	tokenURL    string
	baseDelay   time.Duration
	mu          sync.Mutex
	accessToken string
}

func New(config config.Config) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		config:     config,
		tokenURL:   oauthTokenURL,
		baseDelay:  baseDelay,
	}
}

func (c *Client) Do(request *http.Request) (*http.Response, error) {
	body, err := readBody(request)
	if err != nil {
		return nil, err
	}

	token, err := c.accessTokenFor(request.Context())
	if err != nil {
		return nil, err
	}

	response, err := c.sendWithRetry(request, body, token)
	if err != nil || response.StatusCode != http.StatusUnauthorized {
		return response, err
	}
	response.Body.Close()

	token, err = c.refreshAccessToken(request.Context())
	if err != nil {
		return nil, err
	}

	return c.sendWithRetry(request, body, token)
}

func (c *Client) sendWithRetry(
	request *http.Request,
	body []byte,
	token string,
) (*http.Response, error) {
	return c.retry(request.Context(), request.URL.Path, func() (*http.Response, error) {
		return c.send(request, body, token)
	})
}

// retry repeats transient failures with exponential backoff. After the last
// attempt it returns the final response so callers report its status as usual.
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

// retryDelay honors a Retry-After header in seconds, capped at maxDelay.
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

func (c *Client) accessTokenFor(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" {
		return c.accessToken, nil
	}

	return c.refreshAccessTokenLocked(ctx)
}

func (c *Client) refreshAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.refreshAccessTokenLocked(ctx)
}

func (c *Client) refreshAccessTokenLocked(ctx context.Context) (string, error) {
	form := url.Values{
		"client_id":     {c.config.ClientID},
		"client_secret": {c.config.ClientSecret},
		"refresh_token": {c.config.RefreshToken},
		"grant_type":    {"refresh_token"},
	}

	response, err := c.retry(ctx, "OAuth refresh", func() (*http.Response, error) {
		request, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			c.tokenURL,
			strings.NewReader(form.Encode()),
		)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		return c.httpClient.Do(request)
	})
	if err != nil {
		return "", fmt.Errorf("send OAuth request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OAuth refresh returned HTTP %d", response.StatusCode)
	}

	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil {
		return "", fmt.Errorf("decode OAuth response: %w", err)
	}
	if token.AccessToken == "" {
		return "", fmt.Errorf("OAuth response has no access_token")
	}

	c.accessToken = token.AccessToken
	return c.accessToken, nil
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

func (c *Client) send(
	original *http.Request,
	body []byte,
	token string,
) (*http.Response, error) {
	request := original.Clone(original.Context())
	request.Header = original.Header.Clone()
	request.Header.Set("Authorization", "Bearer "+token)

	if body != nil {
		request.Body = io.NopCloser(bytes.NewReader(body))
		request.ContentLength = int64(len(body))
	} else {
		request.Body = nil
	}

	return c.httpClient.Do(request)
}
