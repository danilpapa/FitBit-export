package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	"fitbit-export/internal/config"
	"fitbit-export/internal/httpclient"
)

const (
	authorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	redirectURL  = "http://localhost:8085/callback"
	listenAddr   = "127.0.0.1:8085"
	loginTimeout = 5 * time.Minute
)

var scopes = []string{
	"https://www.googleapis.com/auth/googlehealth.activity_and_fitness.readonly",
	"https://www.googleapis.com/auth/googlehealth.health_metrics_and_measurements.readonly",
	"https://www.googleapis.com/auth/googlehealth.sleep.readonly",
}

type Result struct {
	RefreshToken  string
	MissingScopes []string
}

type flow struct {
	authorizeURL string
	tokenURL     string
	redirectURL  string
	open         func(target string) error
}

type callback struct {
	code string
	err  error
}

func Login(ctx context.Context, cfg config.Config) (Result, error) {
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return Result{}, fmt.Errorf("listen for OAuth callback: %w", err)
	}

	f := flow{
		authorizeURL: authorizeURL,
		tokenURL:     httpclient.TokenURL,
		redirectURL:  redirectURL,
		open:         openBrowser,
	}
	return f.run(ctx, cfg, listener)
}

func (f flow) run(ctx context.Context, cfg config.Config, listener net.Listener) (Result, error) {
	defer listener.Close()

	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return Result{}, errors.New("GOOGLE_HEALTH_CLIENT_ID and GOOGLE_HEALTH_CLIENT_SECRET are required")
	}

	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()

	state := randomString()
	verifier := randomString()
	callbacks := make(chan callback, 1)

	server := &http.Server{Handler: callbackHandler(state, callbacks)}
	go server.Serve(listener)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	if err := f.open(f.loginURL(cfg, state, verifier)); err != nil {
		log.Printf("open browser: %v", err)
	}

	select {
	case <-ctx.Done():
		return Result{}, fmt.Errorf("wait for browser sign-in: %w", ctx.Err())
	case result := <-callbacks:
		if result.err != nil {
			return Result{}, result.err
		}
		return f.exchange(ctx, cfg, result.code, verifier)
	}
}

func (f flow) loginURL(cfg config.Config, state string, verifier string) string {
	challenge := sha256.Sum256([]byte(verifier))
	query := url.Values{
		"client_id":             {cfg.ClientID},
		"redirect_uri":          {f.redirectURL},
		"response_type":         {"code"},
		"scope":                 {strings.Join(scopes, " ")},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
	}
	return f.authorizeURL + "?" + query.Encode()
}

func callbackHandler(state string, callbacks chan<- callback) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("state") != state {
			http.Error(w, "Invalid state.", http.StatusBadRequest)
			return
		}

		result := parseCallback(query)
		if result.err != nil {
			http.Error(w, "Authorization failed. Return to the terminal.", http.StatusBadRequest)
		} else {
			fmt.Fprintln(w, "Authorization received. Return to the terminal.")
		}

		select {
		case callbacks <- result:
		default:
		}
	})
	return mux
}

func parseCallback(query url.Values) callback {
	if reason := query.Get("error"); reason != "" {
		return callback{err: fmt.Errorf("authorization denied: %s", reason)}
	}
	if code := query.Get("code"); code != "" {
		return callback{code: code}
	}
	return callback{err: errors.New("OAuth callback has no code")}
}

func (f flow) exchange(ctx context.Context, cfg config.Config, code string, verifier string) (Result, error) {
	response, err := httpclient.New(cfg).PostForm(ctx, f.tokenURL, url.Values{
		"client_id":     {cfg.ClientID},
		"client_secret": {cfg.ClientSecret},
		"code":          {code},
		"code_verifier": {verifier},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {f.redirectURL},
	})
	if err != nil {
		return Result{}, fmt.Errorf("send OAuth request: %w", err)
	}
	defer response.Body.Close()

	var token struct {
		RefreshToken     string `json:"refresh_token"`
		Scope            string `json:"scope"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil {
		return Result{}, fmt.Errorf("decode OAuth response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf(
			"OAuth code exchange returned HTTP %d: %s: %s",
			response.StatusCode, token.Error, token.ErrorDescription,
		)
	}
	if token.RefreshToken == "" {
		return Result{}, errors.New("OAuth response has no refresh_token")
	}

	return Result{
		RefreshToken:  token.RefreshToken,
		MissingScopes: missingScopes(token.Scope),
	}, nil
}

func missingScopes(granted string) []string {
	grantedScopes := strings.Fields(granted)

	var missing []string
	for _, scope := range scopes {
		if !slices.Contains(grantedScopes, scope) {
			missing = append(missing, scope)
		}
	}
	return missing
}

func randomString() string {
	buffer := make([]byte, 32)
	rand.Read(buffer)
	return base64.RawURLEncoding.EncodeToString(buffer)
}

func openBrowser(target string) error {
	log.Printf("sign in at: %s", target)

	command := "xdg-open"
	if runtime.GOOS == "darwin" {
		command = "open"
	}
	return exec.Command(command, target).Start()
}
