package fatsecret

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Login links a fatsecret.com account with three-legged OAuth 1.0.
// FatSecret shows a verification code in the browser, and the user types it in.
func (c *Client) Login(ctx context.Context, open func(target string) error, in io.Reader, out io.Writer) (token string, secret string, err error) {
	if c.config.ConsumerKey == "" || c.config.ConsumerSecret == "" {
		return "", "", errors.New("FATSECRET_CONSUMER_KEY and FATSECRET_CONSUMER_SECRET are required")
	}

	requestToken, requestSecret, err := c.tokenStep(ctx, http.MethodPost, c.urls.requestToken, url.Values{
		"oauth_callback": {"oob"},
	}, "")
	if err != nil {
		return "", "", fmt.Errorf("get FatSecret request token: %w", err)
	}

	authorize := c.urls.authorize + "?oauth_token=" + encode(requestToken)
	fmt.Fprintf(out, "Sign in to FatSecret and allow access: %s\n", authorize)
	if err := open(authorize); err != nil {
		fmt.Fprintf(out, "Open the link yourself: %v\n", err)
	}

	fmt.Fprint(out, "Enter the verification code from FatSecret: ")
	line, err := bufio.NewReader(in).ReadString('\n')
	verifier := strings.TrimSpace(line)
	if verifier == "" {
		if err == nil {
			err = errors.New("empty code")
		}
		return "", "", fmt.Errorf("read verification code: %w", err)
	}

	token, secret, err = c.tokenStep(ctx, http.MethodGet, c.urls.accessToken, url.Values{
		"oauth_token":    {requestToken},
		"oauth_verifier": {verifier},
	}, requestSecret)
	if err != nil {
		return "", "", fmt.Errorf("get FatSecret access token: %w", err)
	}
	return token, secret, nil
}

func (c *Client) tokenStep(
	ctx context.Context,
	method string,
	endpoint string,
	params url.Values,
	tokenSecret string,
) (string, string, error) {
	body, status, err := c.send(ctx, method, endpoint, params, tokenSecret)
	if err != nil {
		return "", "", err
	}

	values, parseErr := url.ParseQuery(strings.TrimSpace(string(body)))
	token, secret := values.Get("oauth_token"), values.Get("oauth_token_secret")
	if status != http.StatusOK || parseErr != nil || token == "" || secret == "" {
		return "", "", fmt.Errorf("HTTP %d: %s", status, snippet(body))
	}
	return token, secret, nil
}
