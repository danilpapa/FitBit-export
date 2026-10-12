package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const tokenURL = "https://oauth2.googleapis.com/token"

type Config struct {
	ClientID     string
	ClientSecret string
	RefreshToken string
}

type Token struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

func Refresh(ctx context.Context, cfg Config) (Token, error) {
	form := url.Values {
		"client_id":     {cfg.ClientID},
		"client_secret": {cfg.ClientSecret},
		"refresh_token": {cfg.RefreshToken},
		"grant_type":    {"refresh_token"},
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		tokenURL,
		strings.NewReader(form.Encode()),
	)

	if err != nil {
		return Token{}, fmt.Errorf("create OAuth request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("send OAuth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Token{}, fmt.Errorf("OAuth refresh returned HTTP %d", resp.StatusCode)
	}

	var token Token
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return Token{}, fmt.Errorf("decode OAuth response: %w", err)
	}

	if token.AccessToken == "" {
		return Token{}, fmt.Errorf("OAuth response has no access_token")
	}

	return token, nil
}