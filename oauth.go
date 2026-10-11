
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const tokenURL = "https://oauth2.googleapis.com/token"

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

func RefreshAccessToken() (*TokenResponse, error) {
	form := url.Values{}
	form.Set("client_id", os.Getenv("GOOGLE_HEALTH_CLIENT_ID"))
	form.Set("client_secret", os.Getenv("GOOGLE_HEALTH_CLIENT_SECRET"))
	form.Set("refresh_token", os.Getenv("GOOGLE_HEALTH_REFRESH_TOKEN"))
	form.Set("grant_type", "refresh_token")

	req, err := http.NewRequest(
		http.MethodPost,
		tokenURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return nil, fmt.Errorf(
			"refresh token failed: HTTP %d: %v",
			resp.StatusCode,
			body,
		)
	}

	var tokens TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}

	if tokens.AccessToken == "" {
		return nil, fmt.Errorf("Google returned an empty access token")
	}

	return &tokens, nil
}
