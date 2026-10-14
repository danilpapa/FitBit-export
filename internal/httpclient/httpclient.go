package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"fitbit-export/internal/config"
)

const oauthTokenURL = "https://oauth2.googleapis.com/token"

type Client struct {
	httpClient  *http.Client
	config      config.Config
	mu          sync.Mutex
	accessToken string
}

func New(config config.Config) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		config:     config,
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

	response, err := c.send(request, body, token)
	if err != nil || response.StatusCode != http.StatusUnauthorized {
		return response, err
	}
	response.Body.Close()

	token, err = c.refreshAccessToken(request.Context())
	if err != nil {
		return nil, err
	}

	return c.send(request, body, token)
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

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		oauthTokenURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", fmt.Errorf("create OAuth request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := c.httpClient.Do(request)
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
