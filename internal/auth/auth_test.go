package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"fitbit-export/internal/config"
)

var testConfig = config.Config{ClientID: "client-id", ClientSecret: "client-secret"}

func TestRunExchangesCodeForRefreshToken(t *testing.T) {
	var form url.Values
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		form = r.PostForm
		json.NewEncoder(w).Encode(map[string]string{
			"refresh_token": "new-refresh-token",
			"scope":         strings.Join(scopes, " "),
		})
	}))
	defer tokens.Close()

	var login url.Values
	result, err := runFlow(t, tokens.URL, func(callback string, query url.Values) {
		login = query
		get(t, callback+"?state=wrong&code=forged")
		get(t, callback+"?state="+query.Get("state")+"&code=auth-code")
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}

	if result.RefreshToken != "new-refresh-token" {
		t.Fatalf("RefreshToken = %q", result.RefreshToken)
	}
	if len(result.MissingScopes) != 0 {
		t.Fatalf("MissingScopes = %v, want none", result.MissingScopes)
	}
	if form.Get("code") != "auth-code" {
		t.Fatalf("code = %q, want auth-code", form.Get("code"))
	}
	if form.Get("redirect_uri") != login.Get("redirect_uri") {
		t.Fatalf("redirect_uri = %q, want %q", form.Get("redirect_uri"), login.Get("redirect_uri"))
	}

	challenge := sha256.Sum256([]byte(form.Get("code_verifier")))
	if got := base64.RawURLEncoding.EncodeToString(challenge[:]); got != login.Get("code_challenge") {
		t.Fatalf("code_verifier does not match code_challenge")
	}
	if login.Get("access_type") != "offline" || login.Get("prompt") != "consent" {
		t.Fatalf("login query = %v, want offline access with forced consent", login)
	}
}

func TestRunReportsMissingScopes(t *testing.T) {
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"refresh_token": "new-refresh-token",
			"scope":         scopes[0],
		})
	}))
	defer tokens.Close()

	result, err := runFlow(t, tokens.URL, func(callback string, query url.Values) {
		get(t, callback+"?state="+query.Get("state")+"&code=auth-code")
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if len(result.MissingScopes) != 2 {
		t.Fatalf("MissingScopes = %v, want 2 scopes", result.MissingScopes)
	}
}

func TestRunFails(t *testing.T) {
	tests := []struct {
		name     string
		callback string
		status   int
		response string
		want     string
	}{
		{
			name:     "access denied",
			callback: "error=access_denied",
			want:     "access_denied",
		},
		{
			name:     "exchange rejected",
			callback: "code=auth-code",
			status:   http.StatusBadRequest,
			response: `{"error":"redirect_uri_mismatch","error_description":"Bad Request"}`,
			want:     "redirect_uri_mismatch",
		},
		{
			name:     "no refresh token",
			callback: "code=auth-code",
			status:   http.StatusOK,
			response: `{"access_token":"access"}`,
			want:     "no refresh_token",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				w.Write([]byte(test.response))
			}))
			defer tokens.Close()

			_, err := runFlow(t, tokens.URL, func(callback string, query url.Values) {
				get(t, callback+"?state="+query.Get("state")+"&"+test.callback)
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("run() error = %v, want it to contain %q", err, test.want)
			}
		})
	}
}

func TestRunRequiresClientCredentials(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	f := flow{open: func(string) error {
		t.Fatal("browser must not open without client credentials")
		return nil
	}}
	if _, err := f.run(context.Background(), config.Config{}, listener); err == nil {
		t.Fatal("run() error = nil, want missing credentials error")
	}
}

func runFlow(
	t *testing.T,
	tokenURL string,
	browser func(callback string, query url.Values),
) (Result, error) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	callback := "http://" + listener.Addr().String() + "/callback"
	f := flow{
		authorizeURL: "https://accounts.example/authorize",
		tokenURL:     tokenURL,
		redirectURL:  callback,
		open: func(target string) error {
			parsed, err := url.Parse(target)
			if err != nil {
				return err
			}
			go browser(callback, parsed.Query())
			return nil
		},
	}
	return f.run(context.Background(), testConfig, listener)
}

func get(t *testing.T, target string) {
	response, err := http.Get(target)
	if err != nil {
		t.Errorf("GET %s: %v", target, err)
		return
	}
	response.Body.Close()
}
