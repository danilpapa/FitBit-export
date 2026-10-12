package httpclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"fitbit-export/internal/oauth"
)

type Client struct {
	httpClient *http.Client
	oauth      oauth.Config
	mu          sync.Mutex
	accessToken string
}

func New(cfg oauth.Config) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		oauth: cfg,
	}
}

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	var body []byte
	hasBody := req.Body != nil

	if hasBody {
		var err error
		body, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
	}

	token, err := c.getAccessToken(req.Context())
	if err != nil {
		return nil, err
	}

	resp, err := c.send(req, body, hasBody, token)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	resp.Body.Close()

	c.mu.Lock()

	if c.accessToken == token {
		refreshed, err := oauth.Refresh(req.Context(), c.oauth)
		if err != nil {
			c.mu.Unlock()
			return nil, fmt.Errorf("refresh access token: %w", err)
		}
		c.accessToken = refreshed.AccessToken
	}

	newToken := c.accessToken
	c.mu.Unlock()

	return c.send(req, body, hasBody, newToken)
}

func (c *Client) getAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" {
		return c.accessToken, nil
	}

	token, err := oauth.Refresh(ctx, c.oauth)
	if err != nil {
		return "", fmt.Errorf("get initial access token: %w", err)
	}

	c.accessToken = token.AccessToken
	return c.accessToken, nil
}

func (c *Client) send(
	original *http.Request,
	body []byte,
	hasBody bool,
	token string,
) (*http.Response, error) {
	req := original.Clone(original.Context())
	req.Header = original.Header.Clone()
	req.Header.Set("Authorization", "Bearer "+token)

	if hasBody {
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
	} else {
		req.Body = nil
	}

	return c.httpClient.Do(req)
}